#!/usr/bin/env python3
"""
ocr_rulebook.py — PDF/Markdown → rag_chunks テーブルに挿入するスクリプト。

使い方:
    uv run ocr_rulebook.py <file_path> [--source-type rulebook|correction|houserule] [--db <db_path>]

対応フォーマット: .pdf / .md / .txt
優先度マッピング:
    houserule  → 20
    correction → 10
    rulebook   → 0  (デフォルト)
"""
import argparse
import re
import sqlite3
import sys
from pathlib import Path

try:
    import pdfplumber
except ImportError:
    print("pdfplumber が必要です: uv add pdfplumber", file=sys.stderr)
    sys.exit(1)

PRIORITY_MAP = {"houserule": 20, "correction": 10, "rulebook": 0}
CHUNK_SIZE = 500  # characters per chunk


# ── テキスト抽出 ──────────────────────────────────────────────────────────────

def extract_pdf(path: Path) -> list[str]:
    """PDF から全ページのテキストを抽出してチャンク分割する。"""
    chunks: list[str] = []
    with pdfplumber.open(path) as pdf:
        for page in pdf.pages:
            text = (page.extract_text() or "").strip()
            if text:
                chunks.extend(_split(text))
    return chunks


def extract_markdown(path: Path) -> list[str]:
    """Markdown をセクション単位で分割してからチャンク化する。

    見出し行 (# / ##) をセクション区切りとして使い、意味のまとまりを保つ。
    """
    raw = path.read_text(encoding="utf-8")

    # ページ区切り行（--- のみの行）を除去
    raw = re.sub(r"^\s*---\s*$", "", raw, flags=re.MULTILINE)

    # 見出しでセクション分割
    sections = re.split(r"(?m)^(#{1,3} .+)$", raw)

    chunks: list[str] = []
    current: list[str] = []

    for part in sections:
        part = part.strip()
        if not part:
            continue
        if re.match(r"^#{1,3} ", part):
            # 見出しを次のセクションの先頭として結合
            if current:
                chunks.extend(_split("\n".join(current)))
                current = []
            current.append(part)
        else:
            current.append(part)

    if current:
        chunks.extend(_split("\n".join(current)))

    return [c for c in chunks if c]


def extract_text_file(path: Path) -> list[str]:
    raw = path.read_text(encoding="utf-8").strip()
    return _split(raw)


def _split(text: str) -> list[str]:
    """テキストを CHUNK_SIZE 文字ごとに分割する。"""
    chunks = []
    for i in range(0, len(text), CHUNK_SIZE):
        chunk = text[i : i + CHUNK_SIZE].strip()
        if chunk:
            chunks.append(chunk)
    return chunks


# ── DB 挿入 ───────────────────────────────────────────────────────────────────

def insert_chunks(db_path: Path, chunks: list[str], source_type: str, priority: int, tag: str = "") -> int:
    """チャンクを rag_chunks テーブルに挿入する。embedding は NULL のまま。"""
    con = sqlite3.connect(db_path)
    try:
        cur = con.executemany(
            "INSERT INTO rag_chunks (source_type, priority, tag, text, enabled) VALUES (?, ?, ?, ?, 1)",
            [(source_type, priority, tag, c) for c in chunks],
        )
        con.commit()
        return cur.rowcount
    finally:
        con.close()


# ── エントリポイント ──────────────────────────────────────────────────────────

def main() -> None:
    parser = argparse.ArgumentParser(description="PDF/Markdown → rag_chunks")
    parser.add_argument("file", type=Path, help="入力ファイルパス (.pdf / .md / .txt)")
    parser.add_argument(
        "--source-type",
        choices=list(PRIORITY_MAP),
        default="rulebook",
        help="ソースタイプ (デフォルト: rulebook)",
    )
    parser.add_argument(
        "--tag",
        default="",
        help="トピックタグ（combat/magic/status/general など、任意）",
    )
    parser.add_argument(
        "--db",
        type=Path,
        default=Path(__file__).resolve().parent.parent / "data" / "omni.db",
        help="SQLite DBパス",
    )
    args = parser.parse_args()

    if not args.file.exists():
        print(f"ファイルが見つかりません: {args.file}", file=sys.stderr)
        sys.exit(1)
    if not args.db.exists():
        print(f"DBが見つかりません: {args.db}", file=sys.stderr)
        sys.exit(1)

    suffix = args.file.suffix.lower()
    priority = PRIORITY_MAP[args.source_type]
    print(f"[index] {args.file.name} ({suffix}) → source_type={args.source_type}, priority={priority}")

    if suffix == ".pdf":
        chunks = extract_pdf(args.file)
    elif suffix in (".md", ".markdown"):
        chunks = extract_markdown(args.file)
    elif suffix == ".txt":
        chunks = extract_text_file(args.file)
    else:
        print(f"未対応の拡張子: {suffix}  (.pdf / .md / .txt のみ)", file=sys.stderr)
        sys.exit(1)

    print(f"[index] {len(chunks)} チャンク抽出完了")
    n = insert_chunks(args.db, chunks, args.source_type, priority, args.tag)
    print(f"[index] {n} チャンク挿入完了 → {args.db}")


if __name__ == "__main__":
    main()
