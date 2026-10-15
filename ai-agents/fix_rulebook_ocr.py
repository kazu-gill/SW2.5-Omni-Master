#!/usr/bin/env python3
"""
fix_rulebook_ocr.py — ルールブック Markdown (原本) の OCR 誤字を直す。

使い方:
    uv run ai-agents/fix_rulebook_ocr.py            # 何が直るかを表示するだけ
    uv run ai-agents/fix_rulebook_ocr.py --apply    # 原本をバックアップしてから書き換える

原本 (data/rulebook/*.md) は著作物のため git 管理外で、履歴が残らない。
そのため修正はこの置換表として残し、原本を取り直したときも同じ修正をかけ直せるようにする。

各置換は、誤字が現れる文脈 (「職れ:」のような項目名の位置など) に限定し、件数を固定する。
件数が合わない置換があれば何も書き込まずに止まる (原本が変わった可能性があるため)。
すでに直っている置換 (0 件) は「適用済み」として飛ばすので、何度実行してもよい。

誤字の根拠は docs/ocr-suspects-20260927.md。
"""

import argparse
import re
import shutil
import sys
from collections.abc import Callable
from dataclasses import dataclass
from datetime import datetime
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
DEFAULT_MD = ROOT / "data" / "rulebook" / "swordworld2.5_rulebook1.md"


@dataclass(frozen=True)
class Fix:
    note: str
    pattern: str
    replacement: str | Callable[[re.Match], str]
    count: int  # 原本での件数


# ローマ数字の判定: 直前が日本語 (かな・カナ・漢字) で、直後が英字でない
_JA_BEFORE = r"(?<=[ぁ-ゖァ-ヶー一-龥々])"
_NOT_LATIN = r"(?![A-Za-z])"

# 中黒あり・なしの両方が出てくる語 (中黒付きの形)。中黒を取って揃える
_DOTTED_PAIRS = (
    "ア・ルフレイム",
    "アルフ・レイム",
    "ターゲット・サイト",
    "ファイア・ボール",
    "ウェポン・マスター",
    "クイック・ローダー",
    "クリエイト・ウェポン",
    "ゲーム・データ",
    "リープ・スラッシュ",
    "アース・シールド",
    "マナ・アブソーブ",
    "オート・モビル",
    "ディスガイズ・セット",
)

# 項目名「穢れ」の化け (魔物データ・サンプルキャラクターのシート)
_LABEL = r"(?=\s*[:：])"
FIXES = [
    Fix("穢れ ← 職れ (魔物データ)", r"職（しょく）れ" + _LABEL, "穢（けが）れ", 4),
    Fix("穢れ ← 縄 (魔物データ)", r"縄（なわ）" + _LABEL, "穢れ（けがれ）", 4),
    Fix(
        "穢れ ← 隠れ (魔物データ: ドレイク)",
        r"隠れ（かくれ）" + _LABEL,
        "穢れ（けがれ）",
        2,
    ),
    Fix(
        "穢れ ← 練れ (魔物データ: ディアボロ)",
        r"練れ\(ねれ\)" + _LABEL,
        "穢れ(けがれ)",
        2,
    ),
    Fix("穢れ ← 隠れ / 破れ (サンプルシート)", r"(?:隠|破)れ(?=: 0点)", "穢れ", 2),
    Fix(
        "穢れ ← 慣れ (サンプルシート)",
        r"^## 慣れ（なれ）(?=\r?$)",
        "## 穢れ（けがれ）",
        2,
    ),
    Fix("穢れ ← 慣れ (巻末シート)", r"慣れ（な） \[ \]", "穢れ [ ]", 1),
    Fix("鎧 ← 籠", r"籠(?=（(?:ひきんぞく|きんぞく)?よろい）)", "鎧", 3),
    Fix(
        "知名度 ← 知力度", r"知力度（ちりょくど）(?=／弱点値)", "知名度（ちめいど）", 2
    ),
    Fix("胴体 ← 頭体 (部位名)", r"頭体（とうたい）", "胴体（どうたい）", 4),
    # 「金緑 A」は戦利品の素材ランク表記で正しい。OCR が付けたルビ「きんりょく」だけが誤り
    Fix("金緑 の誤ったルビを削除", r"金緑（きんりょく）", "金緑", 2),
    Fix("マナサーチ ← マナーサーチ", r"マナーサーチ", "マナサーチ", 2),
    Fix(
        "スプリントアーマー ← スプリットアーマー",
        r"スプリットアーマー",
        "スプリントアーマー",
        1,
    ),
    Fix("火トカゲ ← メトカゲ", r"メトカゲ", "火トカゲ", 1),
    Fix(
        "魔法の発動体 ← 駆動体",
        r"駆動（くどう）体（たい）",
        "発動（はつどう）体（たい）",
        1,
    ),
    # ── 表記の揺れ (B) ──────────────────────────────────────────────────────
    # ローマ数字は全角 Ⅰ/Ⅱ/Ⅲ に揃える (半角 I や前の空白は OCR 由来)。長いものから置換する
    Fix("Ⅲ ← III", _JA_BEFORE + r"\s?III" + _NOT_LATIN, "Ⅲ", 1),
    Fix("Ⅱ ← II", _JA_BEFORE + r"\s?II" + _NOT_LATIN, "Ⅱ", 1),
    Fix("Ⅰ ← I (特技名・書名)", _JA_BEFORE + r"\s?I" + _NOT_LATIN, "Ⅰ", 43),
    Fix("Ⅰ ← 1 (《かばう1》)", r"《かばう1》", "《かばうⅠ》", 1),
    # 中黒は取る。中黒あり・なしの両方が出てくる語だけを対象にする
    # (中黒付きしかない語には「ソード・ワールド」のような正式表記もあるため一律には外さない)
    Fix(
        "中黒を取る",
        "|".join(_DOTTED_PAIRS),
        lambda m: m.group(0).replace("・", ""),
        13,
    ),
    Fix(
        "剣の加護の後のスラッシュを全角に",
        r"(剣(?:（[ぁ-ゖ]+）)?の加護(?:（[ぁ-ゖ]+）)?)/",
        r"\1／",
        4,
    ),
    Fix("長音 ← マイナス記号 (セージ・フェンサー)", r"(?<=セ|サ)−(?=ジ|\s|$)", "ー", 3),
    Fix(
        "剣のかけら ← 剣の欠けら",
        r"《剣（けん）の欠（か）けら》",
        "《剣（けん）のかけら》",
        2,
    ),
]


def plan(md: str) -> tuple[str, list[tuple[Fix, int, list[str]]]]:
    """全置換をかけた結果と、置換ごとの (件数, 前後の例) を返す。"""
    results = []
    for fix in FIXES:
        rx = re.compile(fix.pattern, re.MULTILINE)
        examples = [
            md[max(0, m.start() - 12) : m.end() + 8].replace("\n", "⏎")
            for m in rx.finditer(md)
        ]
        md = rx.sub(fix.replacement, md)
        results.append((fix, len(examples), examples[:2]))
    return md, results


def main() -> None:
    parser = argparse.ArgumentParser(description="ルールブック原本の OCR 誤字を直す")
    parser.add_argument("--md", type=Path, default=DEFAULT_MD)
    parser.add_argument(
        "--apply", action="store_true", help="原本を書き換える (事前にバックアップ)"
    )
    args = parser.parse_args()

    # 原本は CRLF。改行を変えないよう、そのまま読み書きする
    with args.md.open(encoding="utf-8", newline="") as f:
        original = f.read()
    fixed, results = plan(original)

    bad = False
    for fix, n, examples in results:
        if n == 0:
            status = "適用済み"
        elif n == fix.count:
            status = f"{n} 件"
        else:
            status = f"件数不一致 ({n} 件、想定 {fix.count} 件)"
            bad = True
        print(f"- {fix.note}: {status}")
        for ex in examples:
            print(f"    …{ex}…")

    if bad:
        sys.exit(
            "[error] 件数が想定と違う置換があります。原本が変わっていないか確認してください (何も書き込んでいません)"
        )
    if fixed == original:
        print("[fix] 直すところはありません")
        return
    if not args.apply:
        print("[fix] 確認のみ (原本は変更していません)。書き換えるには --apply")
        return

    stamp = datetime.now().astimezone().strftime("%Y%m%d-%H%M%S")
    backup = (
        ROOT
        / "data"
        / "backup"
        / "rulebook"
        / f"{args.md.stem}.orig-{stamp}{args.md.suffix}"
    )
    backup.parent.mkdir(parents=True, exist_ok=True)
    shutil.copy2(args.md, backup)
    with args.md.open("w", encoding="utf-8", newline="") as f:
        f.write(fixed)
    print(f"[fix] バックアップ: {backup}")
    print(f"[fix] 書き換えました: {args.md}")
    print("[fix] RAG に反映するには: build_rulebook.py --apply → embed_chunks.py")


if __name__ == "__main__":
    main()
