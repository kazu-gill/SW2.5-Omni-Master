#!/usr/bin/env python3
"""
find_ocr_suspects.py — ルールブック Markdown から OCR 誤字の候補を洗い出す (DB は触らない)。

使い方:
    uv run ai-agents/find_ocr_suspects.py [--md PATH]

検出方法:
1. ルビ: 同じ読みに付いた漢字が、多数派と少数派に割れているもの (例: よろい → 鎧 / 籠)。
   同音異義語 (効果/高価) も混ざるので、人が見て判断する
2. 項目名: 「知名度／弱点値:」のような「ラベル:」のうち、多数派と 1 文字違いの少数派
3. 長音: カタカナ語の長音がマイナス記号・ハイフンになっているもの (セ−ジ)
4. 用語: 《》〈〉【】[] で囲まれた用語のうち、多数派と 1 文字違いの少数派
   (Ⅰ/I/1 の揺れや中黒の有無もここに出る)

結果の判断と一覧は docs/ocr-suspects-20260927.md。
"""

import argparse
import re
from collections import Counter, defaultdict
from pathlib import Path

DEFAULT_MD = (
    Path(__file__).resolve().parent.parent
    / "data"
    / "rulebook"
    / "swordworld2.5_rulebook1.md"
)
_RUBY_RE = re.compile(r"([一-鿿々]{1,6})（([ぁ-ゖー]{2,})）")


def strip_ruby(text: str) -> str:
    return re.sub(r"（[ぁ-ゖー]+）|\([ぁ-ゖー]+\)", "", text)


def one_edit_apart(a: str, b: str) -> bool:
    if a == b or abs(len(a) - len(b)) > 1:
        return False
    if len(a) == len(b):
        return sum(x != y for x, y in zip(a, b, strict=True)) == 1
    short, long_ = sorted((a, b), key=len)
    return any(long_[:i] + long_[i + 1 :] == short for i in range(len(long_)))


def minority_variants(
    counts: Counter, min_major: int, max_minor: int, min_len: int = 2
):
    """(多数派, 件数, 少数派, 件数) を返す。"""
    majors = [(t, n) for t, n in counts.items() if n >= min_major and len(t) >= min_len]
    for minor, m in counts.items():
        if m > max_minor:
            continue
        for major, n in majors:
            if n > m and one_edit_apart(major, minor):
                yield major, n, minor, m


def ruby_splits(md: str):
    by_reading: dict[str, Counter] = defaultdict(Counter)
    for kanji, reading in _RUBY_RE.findall(md):
        by_reading[reading][kanji] += 1
    for reading, c in sorted(by_reading.items(), key=lambda x: -sum(x[1].values())):
        (top, n), *rest = c.most_common()
        minors = [(k, m) for k, m in rest if m <= n / 3]
        if n >= 3 and minors:
            yield reading, top, n, minors


def main() -> None:
    parser = argparse.ArgumentParser(
        description="ルールブックの OCR 誤字候補を一覧する"
    )
    parser.add_argument("--md", type=Path, default=DEFAULT_MD)
    args = parser.parse_args()
    md = args.md.read_text(encoding="utf-8")
    text = strip_ruby(md)

    print("## 1. ルビの読みに対して漢字が割れているもの (読み: 多数派 / 少数派)")
    for reading, top, n, minors in ruby_splits(md):
        print(f"- {reading}: {top}×{n} / " + ", ".join(f"{k}×{m}" for k, m in minors))

    print("\n## 2. 項目名の揺れ (多数派 ← 少数派)")
    labels = Counter(
        re.findall(
            r"^[\s*\-]*\**([一-龥ぁ-ゖァ-ヶー／/]{1,8})\**\s*[:：]", text, re.MULTILINE
        )
    )
    for major, n, minor, m in minority_variants(labels, min_major=10, max_minor=3):
        print(f"- {major}×{n} ← {minor}×{m}")

    print("\n## 3. 長音がマイナス記号・ハイフンのカタカナ語")
    for word, n in Counter(re.findall(r"[ァ-ヶ]+[−‐\-－][ァ-ヶ]*", text)).most_common():
        print(f"- {word} ×{n}")

    print("\n## 4. 括弧で囲まれた用語の揺れ (多数派 ← 少数派)")
    terms = Counter(
        re.findall(r"[《〈【\[［]([^》〉】\]］\n]{2,20})[》〉】\]］]", text)
    )
    for major, n, minor, m in minority_variants(
        terms, min_major=2, max_minor=2, min_len=3
    ):
        print(f"- {major}×{n} ← {minor}×{m}")


if __name__ == "__main__":
    main()
