#!/usr/bin/env python3
"""
build_rulebook.py — ルールブック Markdown から rag_chunks を作り直す。

使い方:
    uv run ai-agents/build_rulebook.py                 # 確認のみ (DB は変更しない)
    uv run ai-agents/build_rulebook.py --apply         # DB のルールブック行を置き換える
    uv run ai-agents/embed_chunks.py                   # その後、埋め込みを作る

--apply は、同じ source (既定 rulebook1) と、source 未設定の旧ルールブック行
(source_type='rulebook') を削除してから新しいチャンクを入れる。ハウスルール・訂正は
残す。実行前に DB を data/backup/ にバックアップする。

チャンクの作り方は rulebook_chunker.py を参照。
"""

import argparse
import json
import sqlite3
import sys
from collections import Counter
from datetime import datetime
from pathlib import Path

from rulebook_chunker import build_chunks

ROOT = Path(__file__).resolve().parent.parent
DEFAULT_MD = ROOT / "data" / "rulebook" / "swordworld2.5_rulebook1.md"
DEFAULT_DB = ROOT / "data" / "omni.db"
# 表紙・目次 (PDF 1〜4) と、奥付・カバー・記入用シート (242〜245)
DEFAULT_SKIP = "1-4,242-245"

NEW_COLUMNS = [
    "ALTER TABLE rag_chunks ADD COLUMN source TEXT NOT NULL DEFAULT ''",
    "ALTER TABLE rag_chunks ADD COLUMN section TEXT NOT NULL DEFAULT ''",
    "ALTER TABLE rag_chunks ADD COLUMN page_from INTEGER",
    "ALTER TABLE rag_chunks ADD COLUMN page_to INTEGER",
]


def parse_pages(spec: str) -> set[int]:
    pages: set[int] = set()
    for part in filter(None, (p.strip() for p in spec.split(","))):
        a, _, b = part.partition("-")
        pages.update(range(int(a), int(b or a) + 1))
    return pages


def ensure_columns(con: sqlite3.Connection) -> None:
    """Go 側の migrateSchema と同じ列を足す (まだ Go で開いていない DB 用)。"""
    for stmt in NEW_COLUMNS:
        try:
            con.execute(stmt)
        except sqlite3.OperationalError as e:
            if "duplicate column name" not in str(e):
                raise


def backup(db_path: Path) -> Path:
    dest = (
        ROOT
        / "data"
        / "backup"
        / f"{db_path.stem}.bak-{datetime.now().astimezone():%Y%m%d-%H%M%S}{db_path.suffix}"
    )
    dest.parent.mkdir(parents=True, exist_ok=True)
    src = sqlite3.connect(db_path)
    try:
        with sqlite3.connect(dest) as dst:
            src.backup(dst)  # WAL 中でも整合したコピーを取る
    finally:
        src.close()
    return dest


def apply(db_path: Path, chunks, source: str) -> None:
    con = sqlite3.connect(db_path)
    try:
        con.execute("PRAGMA foreign_keys = ON")
        ensure_columns(con)
        old_ids = [
            r[0]
            for r in con.execute(
                "SELECT id FROM rag_chunks WHERE source_type = 'rulebook' AND source IN (?, '')",
                (source,),
            )
        ]
        # 訂正・ハウスルールが参照している行を消すと、参照が宙に浮く
        placeholders = ",".join("?" * len(old_ids)) or "NULL"
        refs = con.execute(
            f"SELECT id, overrides_id FROM rag_chunks WHERE overrides_id IN ({placeholders})",
            old_ids,
        ).fetchall()
        if refs:
            print(
                f"[error] 削除対象を参照している訂正/ハウスルールがあります: {refs}",
                file=sys.stderr,
            )
            print(
                "        UI で参照先を付け替えてから再実行してください。",
                file=sys.stderr,
            )
            sys.exit(1)

        with con:
            con.execute(
                f"DELETE FROM rag_chunks_opt WHERE chunk_id IN ({placeholders})",
                old_ids,
            )
            con.execute(f"DELETE FROM rag_chunks WHERE id IN ({placeholders})", old_ids)
            con.executemany(
                """INSERT INTO rag_chunks
                   (source_type, priority, tag, text, enabled, source, section, page_from, page_to)
                   VALUES ('rulebook', 0, ?, ?, ?, ?, ?, ?, ?)""",
                [
                    (
                        c.tag,
                        c.text,
                        int(c.enabled),
                        source,
                        c.section,
                        c.page_from,
                        c.page_to,
                    )
                    for c in chunks
                ],
            )
        print(
            f"[apply] 旧ルールブック行 {len(old_ids)} 件を削除し、{len(chunks)} 件を追加しました"
        )
    finally:
        con.close()


def main() -> None:
    parser = argparse.ArgumentParser(
        description="ルールブック Markdown → rag_chunks (作り直し)"
    )
    parser.add_argument(
        "--md", type=Path, default=DEFAULT_MD, help="ルールブック Markdown"
    )
    parser.add_argument("--db", type=Path, default=DEFAULT_DB, help="SQLite DB")
    parser.add_argument(
        "--source", default="rulebook1", help="rag_chunks.source に入れる名前"
    )
    parser.add_argument(
        "--skip-pages",
        default=DEFAULT_SKIP,
        help="除外する PDF ページ (例: 1-4,242-245)",
    )
    parser.add_argument(
        "--preview",
        type=Path,
        default=ROOT / "logs" / "rulebook-preview.jsonl",
        help="確認用 JSONL",
    )
    parser.add_argument(
        "--apply", action="store_true", help="DB のルールブック行を置き換える"
    )
    args = parser.parse_args()

    if not args.md.exists():
        sys.exit(f"Markdown が見つかりません: {args.md}")

    chunks = build_chunks(
        args.md.read_text(encoding="utf-8"), skip_pages=parse_pages(args.skip_pages)
    )
    lens = sorted(len(c.text) for c in chunks)
    print(
        f"[build] {len(chunks)} チャンク (文字数 最小 {lens[0]} / 中央 {lens[len(lens) // 2]} / 最大 {lens[-1]})"
    )
    for tag, n in Counter(c.tag for c in chunks).most_common():
        print(f"  {tag:10s} {n}")
    disabled = sum(1 for c in chunks if not c.enabled)
    if disabled:
        print(
            f"[build] うち {disabled} 件は無効で取り込む (サンプルシナリオ本文。UI のルール画面で有効にできる)"
        )

    args.preview.parent.mkdir(parents=True, exist_ok=True)
    with args.preview.open("w", encoding="utf-8") as f:
        for c in chunks:
            f.write(json.dumps(c.__dict__, ensure_ascii=False) + "\n")
    print(f"[build] 確認用: {args.preview}")

    if not args.apply:
        print("[build] 確認のみ (DB は変更していません)。反映するには --apply")
        return
    if not args.db.exists():
        sys.exit(f"DB が見つかりません: {args.db}")
    print(f"[apply] バックアップ: {backup(args.db)}")
    apply(args.db, chunks, args.source)
    print("[apply] 次に埋め込みを作ってください: uv run ai-agents/embed_chunks.py")


if __name__ == "__main__":
    main()
