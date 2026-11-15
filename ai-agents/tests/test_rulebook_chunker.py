from rulebook_chunker import (
    Chunk,
    book_pages,
    build_chunks,
    is_form_table,
    normalize_text,
    parse_toc,
    split_long,
    split_pages,
    strip_ruby,
)


def test_strip_ruby_removes_kana_reading_after_kanji():
    assert strip_ruby("主動作（しゅどうさ）の効果（こうか）") == "主動作の効果"
    assert strip_ruby("剣（つるぎ）の世界") == "剣の世界"
    assert strip_ruby("データ（でーた）の読み方") == "データの読み方"
    assert strip_ruby("《かばう（かばう）Ⅰ》") == "《かばうⅠ》"


def test_strip_ruby_keeps_real_parentheticals():
    assert strip_ruby("ゲームマスター（以下、ＧＭ）") == "ゲームマスター（以下、ＧＭ）"
    assert strip_ruby("威力（→142頁）") == "威力（→142頁）"
    assert strip_ruby("2d（2個のサイコロ）") == "2d（2個のサイコロ）"


def test_normalize_text_converts_fullwidth_alnum_only():
    assert (
        normalize_text("ＧＭとＳＷ２．５、ＰＣ（プレイヤー）")
        == "GMとSW2．5、PC（プレイヤー）"
    )


def test_is_form_table_detects_blank_sheet():
    form = [
        "|技能|レベル|",
        "|---|---|",
        "|ファイター|2|",
        "||||",
        "||||",
        "||||",
    ]
    data = [
        "|威力|2|3|4|",
        "|---|---|---|---|",
        "|10|1|2|3|",
        "|20|2|3|4|",
    ]
    assert is_form_table(form)
    assert not is_form_table(data)


def test_split_pages_and_book_pages():
    md = "## ページ 1\n\n表紙\n\n---\n\n## ページ 2\n\n本文\n"
    pages = split_pages(md)
    assert [p for p, _ in pages] == [1, 2]
    assert "---" not in pages[0][1]
    assert book_pages(6) == (8, 9)


TOC_MD = """## ページ 3

目次 CONTENTS

## 第二部 ルール
### 行為判定（こういはんてい） _______ 95
- 行為判定の方法 _______ 95
- 達成値の比べあい _______ 105
### 戦闘のルール _______ 126
- 戦闘の流れ _______ 128

---

## ページ 4

# 目次 CONTENTS

| 項目 | ページ | 項目 | ページ |
|:---|---:|:---|---:|
| 第四部 ワールド | | 魔物 | 418 |
| 剣の世界ラクシア | 342 | 蛮族 | 437 |
"""


def test_parse_toc_reads_list_and_table_forms():
    toc = parse_toc(TOC_MD)
    by_title = {e.title: e for e in toc}
    assert by_title["行為判定の方法"].page == 95
    assert by_title["行為判定の方法"].part == "第二部 ルール"
    assert by_title["行為判定の方法"].chapter == "行為判定"
    assert by_title["戦闘の流れ"].chapter == "戦闘のルール"
    assert by_title["剣の世界ラクシア"].part == "第四部 ワールド"
    assert by_title["蛮族"].page == 437
    assert [e.page for e in toc] == sorted(e.page for e in toc)


def test_split_long_never_cuts_mid_sentence():
    text = "あ" * 300 + "。" + "い" * 300 + "。" + "う" * 300 + "。"
    parts = split_long(text, target=400, maximum=700)
    assert len(parts) == 3
    assert all(p.endswith("。") for p in parts)
    assert "".join(parts) == text


BOOK_MD = (
    TOC_MD
    + """
---

## ページ 51

### 行為判定の方法

行為判定（こういはんてい）は2個のサイコロを振って行います。出目に技能レベルと能力値ボーナスを足したものが達成値（たっせいち）です。

---

## ページ 52

## キャラクター名

## プレイヤー名

|技能|レベル|
|---|---|
||||
||||
||||

### 達成値の比べあい

二者が競う場合は、互いに行為判定を行い、達成値の高いほうが勝ちます。同値の場合は受動側の勝ちとなります。
"""
)


def test_build_chunks_assigns_sections_and_drops_noise():
    chunks = build_chunks(BOOK_MD, skip_pages={3, 4})
    texts = [c.text for c in chunks]
    assert all(isinstance(c, Chunk) for c in chunks)
    assert not any("キャラクター名" in t or "||||" in t for t in texts), texts
    assert not any("_______" in t for t in texts)

    judge = next(c for c in chunks if "2個のサイコロ" in c.text)
    assert judge.section == "第二部 ルール > 行為判定 > 行為判定の方法"
    assert "（こういはんてい）" not in judge.text
    assert (judge.page_from, judge.page_to) == (98, 99)

    versus = next(c for c in chunks if "受動側の勝ち" in c.text)
    assert versus.section.endswith("達成値の比べあい")
    assert versus.tag == "rule"


def test_build_chunks_merges_short_headed_entries():
    glossary = "".join(
        f"### {w}\n\n{w}の説明です。これは用語集の一項目で、短い本文が続きます。\n\n"
        for w in ("ガメル", "1日", "判定", "達成値")
    )
    md = TOC_MD + "\n---\n\n## ページ 51\n\n" + glossary
    chunks = build_chunks(md, skip_pages={3, 4})
    assert len(chunks) == 1
    text = chunks[0].text
    assert "ガメルの説明" in text and "【1日】" in text and "【達成値】" in text


def test_build_chunks_drops_layout_elements():
    body = (
        "### 人間の戦士\n\n「突撃して敵を倒す。それが、仲間を助けるのさ」戦士は前線で仲間を守る頼もしい存在です。"
        "剣と盾を手に、敵の攻撃を一身に受け止めます。\n\n"
        "**サイドバー**\n- はじめに\n- 第1部\n- 第2部\n\n24\n\n"
        "### 能力チャート\n* 腕っぷし\n* 守りの堅さ\n"
    )
    md = TOC_MD + "\n---\n\n## ページ 51\n\n" + body
    texts = [c.text for c in build_chunks(md, skip_pages={3, 4})]
    joined = "\n".join(texts)
    assert "突撃して敵を倒す" in joined
    assert (
        "サイドバー" not in joined and "第1部" not in joined and "腕っぷし" not in joined
    )
    assert not any(line.strip() == "24" for line in joined.splitlines())


def test_is_form_text():
    from rulebook_chunker import is_form_text

    sheet = "先制力: 9\n移動力: 15m\n全力移動: 45m\n命中力: 6\n追加ダメージ: 3\n回避力: 5"
    langs = (
        "エルフは長命な種族で、300歳程度からゆっくりと老い始めます。森の中で暮らすことを好みます。\n"
        "最初に話せる言葉：森の言葉と街の共通語\n覚えられない技能：特になし"
    )
    listing = "- 危険感知判定\n- 探索判定\n- 足跡追跡判定\n- 聞き耳判定\n- 隠密判定"
    assert is_form_text(sheet)
    assert not is_form_text(langs)
    assert not is_form_text(listing)  # 判定の列挙はルール情報なので残す


def test_is_form_table_ignores_blank_header():
    example = [
        "| | | | |",
        "|:---|:---|:---|:---|",
        "| 〈ロングボウ〉で射撃 | 「当たれ」 | 12 | 威力20/C(◎)+4 |",
    ]
    assert not is_form_table(example)


def test_monster_stat_block_is_kept():
    body = (
        "### ゴブリン\n\n知能: 低い\n知覚: 五感（暗視）\n反応: 敵対的\n穢れ: 1\n言語: 汎用蛮族語、妖魔語\n"
        "生息地: 森林、洞窟\n知名度/弱点値: 5/11\n弱点: 命中力+1\n"
    )
    md = TOC_MD + "\n---\n\n## ページ 220\n\n" + body
    texts = [c.text for c in build_chunks(md, skip_pages={3, 4})]
    assert any("知能: 低い" in t for t in texts), texts


def test_is_form_table_keeps_feat_table_with_trailing_empty_columns():
    feat = [
        "| 前提 | なし | 使用 | — |",
        "| :--- | :--- | :--- | :--- |",
        "| 概要 | 誤射を防ぐ | | |",
        "| 効果 | 混戦の中でも、狙った相手だけに矢を届けられます | | |",
    ]
    assert not is_form_table(feat)


def test_is_form_table_keeps_continuation_rows_and_merged_cells():
    spell = [
        "| | 消費 | MP5 |",
        "|:---|:---|:---|",
        "| 効果 | 光の加護で、ひととき感覚を研ぎ澄ませます。 | |",
        "| | 対象の見張りや探し物に関わる判定に+1します。 | |",
        "| | 重ねてかけても、効果は大きくなりません。 | |",
    ]
    weapon = [
        "| 武器攻撃分類 | タイプ | サブタイプ |",
        "|---|---|---|",
        "| | 近接攻撃 | |",
        "| | 遠隔攻撃 | 射撃攻撃 |",
        "| | | 投擲攻撃 |",
    ]
    accessories = [
        "| 部位 | 名称 | 効果 |",
        "|---|---|---|",
        "| 頭 | | |",
        "| 顔 | | |",
        "| 耳 | | |",
    ]
    assert not is_form_table(spell)
    assert not is_form_table(weapon)
    assert is_form_table(accessories)


def test_short_intro_is_prepended_to_next_chunk():
    body = (
        "### 行為判定の方法\n\n行為判定の手順を説明します。\n\n"
        "#### 手順\n\n"
        + "行為判定では2個のサイコロを振り、出目に基準値を足して達成値を求めます。" * 5
        + "\n"
    )
    md = TOC_MD + "\n---\n\n## ページ 51\n\n" + body
    chunks = build_chunks(md, skip_pages={3, 4})
    assert len(chunks) == 1
    assert chunks[0].text.startswith("行為判定の手順を説明します。")


def test_tag_for_sample_scenario():
    from rulebook_chunker import TocEntry, tag_for

    scenario = TocEntry(
        "第五部 ゲームマスター",
        "サンプルシナリオ「蛮族を駆逐せよ」",
        "サンプルシナリオ「蛮族を駆逐せよ」",
        402,
    )
    goblin = TocEntry("第五部 ゲームマスター", "蛮族", "蛮族", 437)
    assert tag_for(scenario) == "scenario"
    assert tag_for(goblin) == "monster"


def test_nested_headings_keep_parent_in_section():
    body = (
        "## 2 ゴブリン\n\n知能: 低い\n知覚: 五感（暗視）\n反応: 敵対的\n\n"
        "#### 戦利品\n\n| 出目 | 戦利品 |\n|---|---|\n| 2~6 | なし |\n| 7~12 | 粗末な武器（10G） |\n"
        "| 13~ | 銀の装飾品（50G） |\n\n"
        "## 3 ボルグ\n\n知能: 低い\n知覚: 五感\n反応: 敵対的\n"
    )
    md = TOC_MD + "\n---\n\n## ページ 220\n\n" + body
    chunks = build_chunks(md, skip_pages={3, 4})
    loot = next(c for c in chunks if "粗末な武器" in c.text)
    assert (
        "ゴブリン" in loot.section
        or "【戦利品】" in loot.text
        and "ゴブリン" in loot.text
    ), loot
    assert not any("ボルグ" in c.section and "ゴブリン" in c.section for c in chunks)
