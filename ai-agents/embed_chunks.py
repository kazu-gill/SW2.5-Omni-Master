#!/usr/bin/env python3
"""
embed_chunks.py — rag_chunks / rag_chunks_opt の未埋め込み行にベクターを生成する。

使い方:
    uv run embed_chunks.py [--db <db_path>] [--embed-url URL] [--embed-model-id ID]
                           [--batch 32] [--reembed-all]

埋め込みモデルを変えたときは --reembed-all で全件を作り直す
(次元数が変わるため、古いベクトルと混在させると検索できない)。
"""

import argparse
import sqlite3
import struct
import sys
from pathlib import Path

from mlx_client import DEFAULT_EMBED_MODEL_ID, DEFAULT_EMBED_URL, embed, ping

# (テーブル, ベクトル化するテキストの列)
TABLES = [("rag_chunks", "text"), ("rag_chunks_opt", "text")]


def float32_to_blob(values: list[float]) -> bytes:
    return struct.pack(f"<{len(values)}f", *values)


def embed_table(
    con: sqlite3.Connection, table: str, url: str, model_id: str, batch: int
) -> tuple[int, int]:
    """table の embedding IS NULL の行をすべて処理し、(成功, 失敗) を返す。"""
    ok = failed = 0
    failed_ids: set[int] = set()
    while True:
        # 失敗した行は NULL のまま残るため、除外しないと同じ行を取り続ける
        placeholders = ",".join("?" * len(failed_ids))
        exclude = f"AND id NOT IN ({placeholders})" if failed_ids else ""
        rows = con.execute(
            f"SELECT id, text FROM {table} WHERE embedding IS NULL {exclude} LIMIT ?",
            (*failed_ids, batch),
        ).fetchall()
        if not rows:
            return ok, failed
        for row_id, text in rows:
            try:
                vec = embed(url, model_id, text)
            except (OSError, ValueError, KeyError, IndexError) as e:
                print(
                    f"  [WARN] {table} id={row_id} 埋め込み失敗: {e}", file=sys.stderr
                )
                failed_ids.add(row_id)
                failed += 1
                continue
            con.execute(
                f"UPDATE {table} SET embedding = ? WHERE id = ?",
                (float32_to_blob(vec), row_id),
            )
            ok += 1
        con.commit()
        print(f"  [{table}] {ok} 件完了 (失敗 {failed})")


def run(db_path: Path, url: str, model_id: str, batch: int, reembed_all: bool) -> None:
    con = sqlite3.connect(db_path)
    try:
        if reembed_all:
            for table, _ in TABLES:
                n = con.execute(f"UPDATE {table} SET embedding = NULL").rowcount
                print(f"[embed] {table}: {n} 件の埋め込みを消去")
            con.commit()

        for table, _ in TABLES:
            ok, failed = embed_table(con, table, url, model_id, batch)
            print(f"[embed] {table}: {ok} 件埋め込み完了 / 失敗 {failed} 件")
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
        default=DEFAULT_EMBED_URL,
        help="mlx-serve 埋め込みプロファイルの URL",
    )
    parser.add_argument(
        "--embed-model-id",
        default=DEFAULT_EMBED_MODEL_ID,
        help="埋め込みプロファイルの model_id",
    )
    parser.add_argument(
        "--batch", type=int, default=32, help="1 回のコミットで処理する件数"
    )
    parser.add_argument(
        "--reembed-all", action="store_true", help="既存の埋め込みを消して全件作り直す"
    )
    args = parser.parse_args()

    if not args.db.exists():
        print(f"DBが見つかりません: {args.db}", file=sys.stderr)
        sys.exit(1)
    if not ping(args.embed_url):
        print(f"埋め込みサーバに接続できません: {args.embed_url}", file=sys.stderr)
        print(
            f"  pwsh /Users/kf/AI/MLX/scripts/Start-MlxServe.ps1 -Profile {args.embed_model_id}",
            file=sys.stderr,
        )
        sys.exit(1)

    run(args.db, args.embed_url, args.embed_model_id, args.batch, args.reembed_all)


if __name__ == "__main__":
    main()
