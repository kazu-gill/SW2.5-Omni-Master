#!/usr/bin/env python3
"""
optimize_chunks.py — rag_chunks をローカル LLM で最適化するバッチスクリプト。

処理フロー:
  rag_chunks (原文) → LLM最適化 → rag_chunks_opt (最適化済み)
                                 → 再埋め込み → rag_chunks_opt.embedding

使い方:
  uv run ai-agents/optimize_chunks.py [オプション]

オプション:
  --limit N          1回の実行で処理する最大チャンク数 (デフォルト: 20)
  --model-url URL    テキスト最適化LLMのURL (デフォルト: http://localhost:11430)
  --embed-url URL    埋め込みLLMのURL (デフォルト: http://localhost:11435)
  --db PATH          DBファイルパス
  --dry-run          DBを更新せず処理内容だけ表示
  --retry-failed     failed ステータスのチャンクを再処理
  --status           処理状況の統計を表示して終了
  --chunk-id N       指定IDのチャンクのみ処理（デバッグ用）
"""
import argparse
import json
import re
import sqlite3
import struct
import sys
import time
import urllib.request
import urllib.error
from pathlib import Path
from typing import Optional

# ── 定数 ─────────────────────────────────────────────────────────────────────

_PROJECT_ROOT = Path(__file__).resolve().parent.parent
DEFAULT_DB    = _PROJECT_ROOT / "data" / "omni.db"
DEFAULT_LLM   = "http://localhost:11430"
DEFAULT_EMBED = "http://localhost:11435"

VALID_TAGS = {"combat", "magic", "character", "item", "world", "general", "status", "rule"}

OPTIMIZE_PROMPT = """\
あなたはSW2.5（ソード・ワールド2.5）ルールブックのテキスト整理アシスタントです。
以下のテキストはPDF→Markdown変換されたルールブックの一部です。

## 作業内容
1. OCRの誤字・文字化けを修正する
2. ルビ表記（漢字（よみがな）形式）は「漢字」のみに整理する（ルビ除去）
3. 不要な記号・装飾・改行を除去して読みやすく整形する
4. ルール上の重要な数値・条件は必ず保持する
5. 以下のタグから最適なものを1つ選ぶ:
   combat / magic / character / item / world / general / status / rule
6. 30文字以内の一行要約を作成する

## 出力形式（JSONのみ。説明文・マークダウン不要）
{"text": "最適化されたテキスト", "tag": "タグ名", "summary": "一行要約"}

## 入力テキスト
{chunk_text}
"""

# ── HTTP ヘルパー ─────────────────────────────────────────────────────────────

def http_post(url: str, payload: dict, timeout: int = 60) -> dict:
    data = json.dumps(payload).encode()
    req = urllib.request.Request(url, data=data,
                                  headers={"Content-Type": "application/json"})
    with urllib.request.urlopen(req, timeout=timeout) as res:
        return json.loads(res.read())


def call_llm(model_url: str, prompt: str, timeout: int = 120) -> Optional[str]:
    """テキスト最適化LLMを呼び出してテキストを返す。"""
    try:
        result = http_post(f"{model_url}/completion", {
            "prompt": prompt,
            "max_tokens": 1024,
            "temperature": 0.1,
            "stop": ["\n\n\n"],
        }, timeout=timeout)
        return result.get("content", "").strip()
    except Exception as e:
        print(f"  [LLM error] {e}", file=sys.stderr)
        return None


def call_embed(embed_url: str, text: str, timeout: int = 30) -> Optional[list[float]]:
    """埋め込みモデルを呼び出してベクトルを返す。"""
    try:
        result = http_post(f"{embed_url}/embedding", {"content": text}, timeout=timeout)
        return result.get("embedding")
    except Exception as e:
        print(f"  [Embed error] {e}", file=sys.stderr)
        return None


def ping(url: str) -> bool:
    try:
        urllib.request.urlopen(f"{url}/health", timeout=3)
        return True
    except Exception:
        return False


# ── JSON パース ───────────────────────────────────────────────────────────────

def parse_llm_output(raw: str) -> Optional[dict]:
    """LLM出力からJSONを抽出する。マークダウンコードブロックも考慮。"""
    # ```json ... ``` を除去
    raw = re.sub(r"```(?:json)?\s*", "", raw).strip().rstrip("`").strip()
    # 最初の { から最後の } を抽出
    m = re.search(r"\{.*\}", raw, re.DOTALL)
    if not m:
        return None
    try:
        parsed = json.loads(m.group())
    except json.JSONDecodeError:
        return None
    if "text" not in parsed or not parsed["text"].strip():
        return None
    tag = str(parsed.get("tag", "general")).lower().strip()
    if tag not in VALID_TAGS:
        tag = "general"
    summary = str(parsed.get("summary", ""))[:50]
    return {"text": parsed["text"].strip(), "tag": tag, "summary": summary}


# ── DB ヘルパー ───────────────────────────────────────────────────────────────

def float32_to_blob(vec: list[float]) -> bytes:
    return struct.pack(f"{len(vec)}f", *vec)


def fetch_pending(con: sqlite3.Connection, limit: int,
                  retry_failed: bool, chunk_id: Optional[int]) -> list[tuple]:
    """処理対象チャンクを取得する。"""
    if chunk_id is not None:
        return con.execute(
            "SELECT id, text, tag FROM rag_chunks WHERE id = ? AND enabled = 1",
            (chunk_id,)
        ).fetchall()
    statuses = ["NULL"]
    if retry_failed:
        statuses.append("'failed'")
    cond = " OR ".join(f"opt_status IS {s}" if s == "NULL" else f"opt_status = {s}"
                       for s in statuses)
    return con.execute(
        f"SELECT id, text, tag FROM rag_chunks WHERE enabled = 1 AND ({cond}) LIMIT ?",
        (limit,)
    ).fetchall()


def upsert_opt(con: sqlite3.Connection, chunk_id: int, text: str, tag: str,
               summary: str, embedding: Optional[list[float]], model: str) -> None:
    blob = float32_to_blob(embedding) if embedding else None
    con.execute("""
        INSERT INTO rag_chunks_opt (chunk_id, text, tag, summary, embedding, model)
        VALUES (?, ?, ?, ?, ?, ?)
        ON CONFLICT(chunk_id) DO UPDATE SET
          text=excluded.text, tag=excluded.tag, summary=excluded.summary,
          embedding=excluded.embedding, model=excluded.model,
          optimized_at=CURRENT_TIMESTAMP
    """, (chunk_id, text, tag, summary, blob, model))
    con.execute("UPDATE rag_chunks SET opt_status='done' WHERE id=?", (chunk_id,))


def mark_failed(con: sqlite3.Connection, chunk_id: int) -> None:
    con.execute("UPDATE rag_chunks SET opt_status='failed' WHERE id=?", (chunk_id,))


def show_stats(con: sqlite3.Connection) -> None:
    rows = con.execute("""
        SELECT COALESCE(opt_status, 'unprocessed') AS status, count(*) AS cnt
        FROM rag_chunks WHERE enabled = 1
        GROUP BY opt_status ORDER BY cnt DESC
    """).fetchall()
    total = sum(r[1] for r in rows)
    opt_count = con.execute(
        "SELECT count(*) FROM rag_chunks_opt WHERE embedding IS NOT NULL"
    ).fetchone()[0]
    print("\n── 最適化ステータス ─────────────────────────────")
    for status, cnt in rows:
        bar = "█" * min(30, int(cnt / max(total, 1) * 30))
        print(f"  {status:<15} {cnt:>5}件  {bar}")
    print(f"  {'(合計)':<15} {total:>5}件")
    print(f"  再埋め込み完了: {opt_count}件")
    print("─────────────────────────────────────────────────\n")


# ── メイン ────────────────────────────────────────────────────────────────────

def main() -> None:
    parser = argparse.ArgumentParser(description="rag_chunks をLLMで最適化するバッチ")
    parser.add_argument("--limit",        type=int,  default=20,          help="処理件数上限 (デフォルト: 20)")
    parser.add_argument("--model-url",    default=DEFAULT_LLM,            help="テキスト最適化LLM URL")
    parser.add_argument("--embed-url",    default=DEFAULT_EMBED,          help="埋め込みLLM URL")
    parser.add_argument("--db",           type=Path, default=DEFAULT_DB,  help="DB ファイルパス")
    parser.add_argument("--dry-run",      action="store_true",            help="DBを更新しない")
    parser.add_argument("--retry-failed", action="store_true",            help="failed チャンクを再処理")
    parser.add_argument("--status",       action="store_true",            help="統計を表示して終了")
    parser.add_argument("--chunk-id",     type=int,  default=None,        help="指定IDのみ処理")
    args = parser.parse_args()

    if not args.db.exists():
        print(f"[error] DB が見つかりません: {args.db}", file=sys.stderr)
        sys.exit(1)

    con = sqlite3.connect(args.db)
    con.row_factory = sqlite3.Row

    if args.status:
        show_stats(con)
        con.close()
        return

    # ── サービス死活確認 ──
    llm_ok   = ping(args.model_url)
    embed_ok = ping(args.embed_url)
    print(f"[check] LLM   ({args.model_url}): {'✓ 接続OK' if llm_ok else '✗ 未接続'}")
    print(f"[check] Embed ({args.embed_url}): {'✓ 接続OK' if embed_ok else '✗ 未接続（埋め込みをスキップ）'}")
    if not llm_ok:
        print("[error] LLM に接続できません。llama-server が起動しているか確認してください。")
        sys.exit(1)

    # ── 処理対象取得 ──
    pending = fetch_pending(con, args.limit, args.retry_failed, args.chunk_id)
    if not pending:
        print("[info] 処理対象チャンクがありません。")
        show_stats(con)
        con.close()
        return

    print(f"\n[batch] {len(pending)} 件を処理します"
          + (" (dry-run)" if args.dry_run else "") + "\n")

    ok_count = fail_count = skip_count = 0
    t_start = time.time()

    for i, row in enumerate(pending, 1):
        chunk_id, orig_text, orig_tag = row[0], row[1], row[2]
        short = orig_text[:60].replace("\n", " ")
        print(f"  [{i:>3}/{len(pending)}] ID:{chunk_id}  {short!r}")

        # ── LLM最適化 ──
        prompt = OPTIMIZE_PROMPT.format(chunk_text=orig_text)
        raw = call_llm(args.model_url, prompt)
        if raw is None:
            print(f"          → LLM失敗")
            if not args.dry_run:
                mark_failed(con); con.commit()
            fail_count += 1
            continue

        parsed = parse_llm_output(raw)
        if parsed is None:
            print(f"          → JSON解析失敗: {raw[:80]!r}")
            if not args.dry_run:
                mark_failed(con); con.commit()
            fail_count += 1
            continue

        opt_text = parsed["text"]
        tag      = parsed["tag"]
        summary  = parsed["summary"]
        print(f"          → tag:{tag}  summary:{summary!r}")

        # ── 再埋め込み ──
        embedding = None
        if embed_ok:
            embedding = call_embed(args.embed_url, opt_text)
            if embedding is None:
                print(f"          → 埋め込み失敗（テキストのみ保存）")

        # ── DB 保存 ──
        if not args.dry_run:
            upsert_opt(con, chunk_id, opt_text, tag, summary, embedding,
                       model=args.model_url)
            con.commit()

        ok_count += 1

    elapsed = time.time() - t_start
    print(f"\n── 結果 ──────────────────────────────────────────")
    print(f"  成功: {ok_count}件  失敗: {fail_count}件  スキップ: {skip_count}件")
    print(f"  経過時間: {elapsed:.1f}秒  ({elapsed/max(ok_count+fail_count,1):.1f}秒/件)")
    if not args.dry_run:
        show_stats(con)

    con.close()


if __name__ == "__main__":
    main()
