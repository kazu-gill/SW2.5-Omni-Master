#!/usr/bin/env python3
"""
embed_chunks.py — rag_chunks テーブル内の未埋め込みチャンクにベクターを生成する。

使い方:
    uv run embed_chunks.py [--db <db_path>] [--embed-url <llama-server URL>] [--batch 32]
"""
import argparse
import sqlite3
import struct
import sys
from pathlib import Path

try:
    import requests
except ImportError:
    print("requests が必要です: uv add requests", file=sys.stderr)
    sys.exit(1)


def get_embedding(text: str, embed_url: str) -> list[float] | None:
    """llama-server の /embedding エンドポイントからベクターを取得する。"""
    try:
        resp = requests.post(
            f"{embed_url}/embedding",
            json={"content": text},
            timeout=30,
        )
        resp.raise_for_status()
        return resp.json().get("embedding")
    except Exception as e:
        print(f"  [WARN] embedding 失敗: {e}", file=sys.stderr)
        return None


def float32_to_blob(values: list[float]) -> bytes:
    return struct.pack(f"<{len(values)}f", *values)


def run(db_path: Path, embed_url: str, batch: int) -> None:
    con = sqlite3.connect(db_path)
    try:
        rows = con.execute(
            "SELECT id, text FROM rag_chunks WHERE embedding IS NULL LIMIT ?", (batch,)
        ).fetchall()

        if not rows:
            print("[embed] 未処理チャンクなし")
            return

        print(f"[embed] {len(rows)} チャンクを処理します...")
        ok = 0
        for chunk_id, text in rows:
            vec = get_embedding(text, embed_url)
            if vec is None:
                continue
            blob = float32_to_blob(vec)
            con.execute("UPDATE rag_chunks SET embedding = ? WHERE id = ?", (blob, chunk_id))
            ok += 1

        con.commit()
        print(f"[embed] {ok}/{len(rows)} チャンク埋め込み完了")
    finally:
        con.close()


def main() -> None:
    parser = argparse.ArgumentParser(description="rag_chunks → embedding 生成")
    parser.add_argument(
        "--db",
        type=Path,
        default=Path(__file__).resolve().parent.parent / "data" / "omni.db",
    )
    parser.add_argument(
        "--embed-url",
        default="http://localhost:11431",
        help="llama-server URL (サポートLM推奨)",
    )
    parser.add_argument("--batch", type=int, default=32, help="1回の処理チャンク数")
    args = parser.parse_args()

    if not args.db.exists():
        print(f"DBが見つかりません: {args.db}", file=sys.stderr)
        sys.exit(1)

    run(args.db, args.embed_url, args.batch)


if __name__ == "__main__":
    main()
