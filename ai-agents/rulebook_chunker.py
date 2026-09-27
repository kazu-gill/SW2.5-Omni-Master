"""rulebook_chunker.py — OCR 済みルールブック Markdown を RAG 用チャンクに組み立てる。

入力 Markdown の前提 (swordworld2.5_rulebook1.md):
- `## ページ N` が PDF のページ区切り。PDF 1 ページに本の見開き 2 ページが入る
  (本のページ = 2N-4, 2N-3)
- PDF 3〜4 ページが目次。「項目 _______ 頁」のリスト形式と、表形式が混在する
- 本文の漢字に「漢字（かな）」形式のルビが付いている

出力チャンクは見出し・段落単位で、文の途中では切らない。各チャンクに
「部 > 章 > 見出し」と本のページを付ける。ページ番号だけの断片、目次、
空欄の記入用シート、チェック欄はチャンクにしない。
"""

import bisect
import hashlib
import re
from dataclasses import dataclass

TARGET_CHARS = 600  # これを超えたら次の区切りでチャンクを閉じる
MAX_CHARS = 900  # 1 段落がこれを超えたら分割する
# 表は数字が多くトークンが増えやすい。埋め込みの上限 (mlx-serve bge-m3 は 512 トークン) に収まるよう小さく切る
MAX_TABLE_CHARS = 450
MIN_BODY_CHARS = 40  # 見出しを除いた本文がこれ未満のチャンクは捨てる
# 小見出しでチャンクを閉じるのは、ここまで溜まってから (用語集のような「見出し + 1 行」の連続をまとめる)
MIN_CLOSE_CHARS = 250

# 見出しの前後に付く飾り (OCR 由来の太字・番号)
_HEADING_RE = re.compile(r"^(#{1,6})\s+(.*?)\s*$")
_PAGE_RE = re.compile(r"^##\s*ページ\s*(\d+)\s*$", re.MULTILINE)
_TOC_LEADER_RE = re.compile(r"_{3,}\s*\d+\s*$")
_RUBY_RE = re.compile(r"([一-鿿々ヶァ-ヺー]+)[（(]([ぁ-ゖー]+)[）)]")
_FULLWIDTH_ALNUM = {
    c: chr(c - 0xFEE0)
    for c in [*range(0xFF10, 0xFF1A), *range(0xFF21, 0xFF3B), *range(0xFF41, 0xFF5B)]
}
_NOISE_LINE_RES = [
    re.compile(r"^-{3,}$"),  # ページ区切り
    re.compile(r"^\**\s*Page\s*\d+\s*\**.*$", re.IGNORECASE),  # 印刷ページ番号
    re.compile(r"^イラスト\s*■"),  # イラストのクレジット
    re.compile(
        r"©\s*\d{4}\s*GROUP\s*SNE", re.IGNORECASE
    ),  # キャラクターシートのコピーライト
    re.compile(r"^(\[\s*\]\s*)+$"),  # チェック欄
    _TOC_LEADER_RE,
    re.compile(r"^\d{1,3}$"),  # ページ番号だけの行 (フッター)
]
# 誌面のレイアウト要素。この見出し (または太字だけの行) から次の見出しまでを捨てる
_LAYOUT_RE = re.compile(
    r"^\**\s*(サイドバー|(ページ)?ナビゲーション|(ページ)?フッター|能力(レーダー)?チャート.*|能力グラフ.*|ページ構成)\s*\**$"
)
# 章題にこれを含む部分はチャンクにしない
EXCLUDED_CHAPTER_WORDS = ("目次", "索引", "デザイナーズノート", "スタッフリスト")
# 章題がこれと一致する部分はチャンクにしない (「キャラクターシートの見かた」は本文なので残す)
EXCLUDED_CHAPTER_TITLES = ("キャラクターシート",)

_MONSTER_WORDS = (
    "魔物",
    "蛮族",
    "動物",
    "植物",
    "アンデッド",
    "魔法生物",
    "魔動機",
    "幻獣",
    "魔神",
    "人族",
    "ファミリア",
)


@dataclass(frozen=True)
class TocEntry:
    part: str
    chapter: str
    title: str
    page: int


@dataclass
class Chunk:
    section: str  # 部 > 章 > 見出し
    text: str
    page_from: int  # 本のページ
    page_to: int
    tag: str
    # 無効で取り込むチャンク (サンプルシナリオ本文など)。UI のルール画面で有効にできる
    enabled: bool = True

    @property
    def content_hash(self) -> str:
        return hashlib.sha1(re.sub(r"\s+", "", self.text).encode()).hexdigest()


# ── テキスト正規化 ────────────────────────────────────────────────────────────


def strip_ruby(text: str) -> str:
    """「漢字（かな）」形式のルビを取り除く。かな以外を含む括弧はそのまま残す。"""
    return _RUBY_RE.sub(r"\1", text)


def normalize_text(text: str) -> str:
    """全角英数字だけを半角にする (句読点・括弧などの全角記号は残す)。"""
    return text.translate(_FULLWIDTH_ALNUM)


def clean_title(text: str) -> str:
    title = normalize_text(strip_ruby(text))
    title = _TOC_LEADER_RE.sub("", title)
    title = re.sub(r"[*_`]+", "", title)
    title = re.sub(r"^[-・●■◆]\s*", "", title)
    return title.strip()


def _title_key(text: str) -> str:
    return re.sub(r"[\s『』「」“”\"'・、。《》〈〉【】()（）]", "", clean_title(text))


# ── ページ・目次 ──────────────────────────────────────────────────────────────


def book_pages(pdf_page: int) -> tuple[int, int]:
    """PDF のページ (見開き) に入っている本のページ。"""
    return 2 * pdf_page - 4, 2 * pdf_page - 3


def split_pages(md: str) -> list[tuple[int, str]]:
    """`## ページ N` で区切り、(PDF ページ, 本文) のリストを返す。"""
    marks = list(_PAGE_RE.finditer(md))
    pages = []
    for i, m in enumerate(marks):
        end = marks[i + 1].start() if i + 1 < len(marks) else len(md)
        body = md[m.end() : end]
        body = "\n".join(line for line in body.splitlines() if line.strip() != "---")
        pages.append((int(m.group(1)), body))
    return pages


def parse_toc(md: str, toc_pages: tuple[int, ...] = (3, 4)) -> list[TocEntry]:
    """目次ページから (部, 章, 項目, 本のページ) を取り出し、ページ順に並べて返す。"""
    raw: list[tuple[str, str, str, int]] = []
    part = chapter = ""
    for pdf_page, body in split_pages(md):
        if pdf_page not in toc_pages:
            continue
        table_rows = []
        for line in body.splitlines():
            s = line.strip()
            if s.startswith("|"):
                table_rows.append([c.strip() for c in s.strip("|").split("|")])
                continue
            h = _HEADING_RE.match(s)
            item = s.startswith("- ")
            if not (h or item):
                continue
            text = h.group(2) if h else s[2:]
            m = re.search(r"_{3,}\s*(\d+)\s*$", text)
            title = clean_title(text)
            if not m:
                if re.match(r"^(第.部|はじめに)", title):
                    part, chapter = title, ""
                continue
            page = int(m.group(1))
            if h:  # 見出しに頁が付いていれば章
                chapter = title
            raw.append((part, chapter or title, title, page))
        # 表形式: 左列を上から、次に右列を上から読む
        cols: list[list[tuple[str, str]]] = [[], []]
        for row in table_rows:
            if len(row) < 4 or set(row[0]) <= set(":- ") or row[0] == "項目":
                continue
            cols[0].append((row[0], row[1]))
            cols[1].append((row[2], row[3]))
        for col in cols:
            for name, page_s in col:
                title = clean_title(name)
                if not title:
                    continue
                if not page_s.strip().isdigit():
                    if re.match(r"^第.部", title):
                        part = title
                    continue
                raw.append((part, title, title, int(page_s)))
    entries = [TocEntry(p, c, t, pg) for p, c, t, pg in raw]
    entries.sort(key=lambda e: e.page)
    return entries


def tag_for(entry: TocEntry | None) -> str:
    if entry is None:
        return "general"
    part, chapter = entry.part, entry.chapter
    if part.startswith("はじめに"):
        return "intro"
    if part.startswith("第一部"):
        return "character"
    if part.startswith("第三部"):
        if "魔法" in chapter or "魔動機械" in chapter:
            return "magic"
        if "戦闘特技" in chapter:
            return "combat"
        return "item"
    if part.startswith("第二部"):
        if "魔法" in chapter:
            return "magic"
        if "戦闘" in chapter:
            return "combat"
        if "アイテム" in chapter or "装備" in chapter:
            return "item"
        return "rule"
    if part.startswith("第四部"):
        return "world"
    if part.startswith("第五部"):
        if entry.title.startswith("サンプルシナリオ「"):
            return "scenario"
        if any(w in chapter for w in _MONSTER_WORDS):
            return "monster"
        return "gm"
    return "general"


# ── ブロック分割とフィルタ ────────────────────────────────────────────────────


def _is_separator_row(row: str) -> bool:
    s = row.strip()
    return "-" in s and set(s) <= set("|:- ")


def is_form_table(rows: list[str]) -> bool:
    """記入用シートの表か。本体の行の半分以上が空欄 (または 1 列目のラベルだけ) なら記入用とみなす。

    データ表でも末尾に空の列が付くこと (OCR) や、見出し行が空のことがあるので、
    セル単位ではなく行単位で判定する。
    """
    sep = next((i for i, r in enumerate(rows) if _is_separator_row(r)), None)
    body_rows = [
        r
        for r in (rows[sep + 1 :] if sep is not None else rows)
        if not _is_separator_row(r)
    ]
    if len(body_rows) < 2:
        return False
    blank = 0
    for row in body_rows:
        s = row.strip()
        s = s.removeprefix("|")
        s = s.removesuffix("|")
        cells = [c.strip() for c in s.split("|")]
        filled = [i for i, c in enumerate(cells) if c]
        # 全セルが空か、1 列目の短いラベルだけ (「頭」「顔」など記入欄の見出し) なら空欄行
        if not filled or (filled == [0] and len(cells) >= 2 and len(cells[0]) <= 12):
            blank += 1
    return blank / len(body_rows) >= 0.5


def _is_noise_line(line: str) -> bool:
    s = line.strip()
    return any(r.search(s) for r in _NOISE_LINE_RES)


@dataclass
class _Block:
    kind: str  # "heading" / "para" / "table"
    text: str
    pdf_page: int
    level: int = 0


def _flush(
    blocks: list[_Block], para: list[str], table: list[str], pdf_page: int
) -> None:
    """溜めた段落・表をブロックにして空にする。記入用シートの表は捨てる。"""
    if para:
        blocks.append(_Block("para", "\n".join(para), pdf_page))
        para.clear()
    if table:
        if not is_form_table(table):
            blocks.append(_Block("table", "\n".join(table), pdf_page))
        table.clear()


def _blocks(pages: list[tuple[int, str]]) -> list[_Block]:
    blocks: list[_Block] = []
    for pdf_page, body in pages:
        para: list[str] = []
        table: list[str] = []

        def flush(para=para, table=table, pdf_page=pdf_page):
            _flush(blocks, para, table, pdf_page)

        skipping = False  # レイアウト要素の中
        for line in body.splitlines():
            s = line.rstrip()
            h = _HEADING_RE.match(s.strip())
            title = clean_title(h.group(2) if h else s)
            if _LAYOUT_RE.match(title):
                flush()
                skipping = True
                continue
            if h:
                skipping = False
            if skipping or not s.strip() or _is_noise_line(s):
                flush()
                continue
            if s.lstrip().startswith("|"):
                if para:
                    flush()
                table.append(s.strip())
                continue
            if table:
                flush()
            if h:
                flush()
                blocks.append(
                    _Block("heading", h.group(2), pdf_page, level=len(h.group(1)))
                )
                continue
            para.append(s.strip())
        flush()
    return blocks


def split_long(
    text: str, target: int = TARGET_CHARS, maximum: int = MAX_CHARS
) -> list[str]:
    """長い段落を文末 (。！？) で分割する。1 文が maximum を超える場合だけ文中で切る。"""
    if len(text) <= maximum:
        return [text]
    sentences = re.findall(r"[^。！？]*[。！？]+[」』）]*|[^。！？]+$", text)
    parts: list[str] = []
    cur = ""
    for s in sentences:
        if cur and len(cur) + len(s) > target:
            parts.append(cur)
            cur = ""
        while len(s) > maximum:
            parts.append(s[:maximum])
            s = s[maximum:]
        cur += s
    if cur:
        parts.append(cur)
    return parts


def _split_table(text: str, maximum: int = MAX_TABLE_CHARS) -> list[str]:
    """長い表を行単位で分割し、各片に見出し行と区切り行を付け直す。"""
    rows = text.splitlines()
    if len(text) <= maximum or len(rows) < 4:
        return [text]
    head = rows[:2] if _is_separator_row(rows[1]) else rows[:1]
    body = rows[len(head) :]
    parts, cur = [], list(head)
    for row in body:
        if len("\n".join(cur)) + len(row) > maximum and len(cur) > len(head):
            parts.append("\n".join(cur))
            cur = list(head)
        cur.append(row)
    parts.append("\n".join(cur))
    return parts


# ── チャンク組み立て ──────────────────────────────────────────────────────────


_FORM_LINE_RE = re.compile(
    r"^[-*\s]*(\[\s*\]\s*)?(\*\*)?[^:：=|。]{1,20}?(\*\*)?\s*([:：=]\s*.{0,15}|\(\s*\)|（\s*）)\s*$"
)


def is_form_text(text: str) -> bool:
    """「先制力: 13」「HP: 19」のような記入欄の行が半分以上を占めるか (記入済みキャラクターシート)。"""
    lines = [
        ln.strip()
        for ln in text.splitlines()
        if ln.strip() and not ln.strip().startswith("【")
    ]
    if len(lines) < 5:
        return False
    formy = sum(
        1
        for ln in lines
        if "。" not in ln and not ln.startswith("|") and _FORM_LINE_RE.match(ln)
    )
    return formy / len(lines) >= 0.5


def prose_ratio(text: str) -> float:
    """文 (「。」を含む行) が本文に占める割合。"""
    lines = [ln for ln in text.splitlines() if ln.strip()]
    total = sum(len(ln) for ln in lines)
    return sum(len(ln) for ln in lines if "。" in ln) / total if total else 0.0


# 「かんたん作成」のうち、サンプルキャラクター本人ではない項目
_EASY_CREATION_GUIDES = (
    "かんたん作成とじっくり作成",
    "サンプルキャラクターを選ぶ",
    "キャラクターシートのコピー",
    "プロフィールをまとめる",
)


def _is_sample_sheet(entry: TocEntry | None, text: str) -> bool:
    """サンプルキャラクターの記入済みシート部分か (解説の文章は残す)。

    魔物データの「知能: 低い」なども記入欄と同じ形なので、この章以外には使わない。
    """
    if (
        entry is None
        or entry.chapter != "かんたん作成"
        or entry.title in _EASY_CREATION_GUIDES
    ):
        return False
    return is_form_text(text) or prose_ratio(text) < 0.5


def _body_len(text: str) -> int:
    lines = [ln for ln in text.splitlines() if not ln.startswith("#")]
    return len(re.sub(r"[\s|:\-*#]", "", "\n".join(lines)))


def build_chunks(md: str, skip_pages: set[int] | None = None) -> list[Chunk]:
    """Markdown 全体からチャンクを作る。skip_pages は除外する PDF ページ (表紙・目次など)。"""
    toc = parse_toc(md)
    toc_pages = [e.page for e in toc]
    toc_by_key: dict[str, list[TocEntry]] = {}
    for e in toc:
        toc_by_key.setdefault(_title_key(e.title), []).append(e)

    skip = skip_pages or set()
    pages = [(p, body) for p, body in split_pages(md) if p not in skip]

    chunks: list[Chunk] = []
    chunk_entries: list[
        TocEntry | None
    ] = []  # chunks と同じ並びで、各チャンクの目次項目
    carry: list = []  # [目次項目, 本文, 開始ページ]: 次のチャンクの頭に付ける短い導入文
    entry: TocEntry | None = None
    heading = ""
    buf: list[str] = []
    buf_pages: list[int] = []

    def section_path() -> str:
        if entry is None:
            return heading
        parts = [entry.part, entry.chapter]
        if entry.title != entry.chapter:
            parts.append(entry.title)
        if heading and _title_key(heading) not in {_title_key(p) for p in parts}:
            parts.append(heading)
        return " > ".join(p for p in parts if p)

    def close():
        if not buf:
            return
        text = normalize_text(strip_ruby("\n\n".join(buf))).strip()
        excluded = entry is not None and (
            any(w in entry.chapter or w in entry.title for w in EXCLUDED_CHAPTER_WORDS)
            or entry.title in EXCLUDED_CHAPTER_TITLES
        )
        first, last = book_pages(min(buf_pages))[0], book_pages(max(buf_pages))[1]
        if excluded or _is_sample_sheet(entry, text):
            pass
        elif _body_len(text) >= MIN_BODY_CHARS:
            if carry and (carry[0] == entry or first - carry[2] <= 2):
                text = carry[1] + "\n\n" + text
                first = min(first, carry[2])
            carry.clear()
            tag = tag_for(entry)
            # サンプルシナリオの本文はルールではなく物語。GM が今のセッションに持ち込まないよう無効で入れる
            chunks.append(
                Chunk(section_path(), text, first, last, tag, enabled=tag != "scenario")
            )
            chunk_entries.append(entry)
        elif (
            chunks
            and chunk_entries[-1] == entry
            and _body_len(text) > 0
            and len(chunks[-1].text) + len(text) <= MAX_CHARS
        ):
            # 節の最後に残った短い本文は、同じ節の前のチャンクに繋げる
            prev = chunks[-1]
            label = (
                f"【{heading}】\n" if heading and heading not in prev.section else ""
            )
            prev.text += "\n\n" + label + text
            prev.page_to = max(prev.page_to, last)
        elif _body_len(text) > 0:
            # 節の冒頭の短い導入文は、同じ節 (または 2 頁以内) の次のチャンクの頭に付ける
            carry[:] = [entry, text, first]
        buf.clear()
        buf_pages.clear()

    current_pdf_page = None
    # 目次項目より下の見出しの階層 [(レベル, 見出し)]。「2 ゴブリン > 戦利品」のようにパスにする
    stack: list[tuple[int, str]] = []
    pending = False  # 前回の本文のあとに見出しが来た (本文が来るまで保留)
    for b in _blocks(pages):
        if b.pdf_page != current_pdf_page:
            # ページが変わったら、見開き左ページの目次項目に合わせる
            current_pdf_page = b.pdf_page
            left, _ = book_pages(b.pdf_page)
            i = bisect.bisect_right(toc_pages, left) - 1
            new_entry = toc[i] if i >= 0 else None
            if (
                new_entry != entry
                and new_entry is not None
                and (entry is None or new_entry.page > entry.page)
            ):
                close()
                entry = new_entry
        if b.kind == "heading":
            # 見出しが目次項目と一致すれば (見開きの近く ±6 頁)、そこから新しい項目
            left, right = book_pages(b.pdf_page)
            match = next(
                (
                    c
                    for c in toc_by_key.get(_title_key(b.text), [])
                    if left - 6 <= c.page <= right + 6
                ),
                None,
            )
            if match is not None:
                close()
                entry, heading, pending = match, "", False
                stack.clear()
                continue
            title = clean_title(b.text)
            if title:
                while stack and stack[-1][0] >= b.level:
                    stack.pop()
                stack.append((b.level, title))
                # 本文が続いたときだけ使う (本文のない見出しの連続は記入欄のラベルなので、次の見出しで消える)
                pending = True
            continue
        if pending and stack:
            if sum(len(x) for x in buf) >= MIN_CLOSE_CHARS:
                close()
            if not buf:
                heading = " > ".join(t for _, t in stack)
            else:
                buf.append(f"【{stack[-1][1]}】")
                buf_pages.append(b.pdf_page)
        pending = False
        pieces = _split_table(b.text) if b.kind == "table" else split_long(b.text)
        for piece in pieces:
            if buf and sum(len(x) for x in buf) + len(piece) > TARGET_CHARS:
                close()
            buf.append(piece)
            buf_pages.append(b.pdf_page)
    close()

    # 同一内容の重複を除く (空白の違いは無視)
    seen: set[str] = set()
    unique = []
    for c in chunks:
        if c.content_hash not in seen:
            seen.add(c.content_hash)
            unique.append(c)
    return unique
