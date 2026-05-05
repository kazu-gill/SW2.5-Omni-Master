#!/usr/bin/env python3
"""
generate_scenarios.py — シナリオと関連素材をオフラインバッチ生成するスクリプト。

処理フロー:
  1. LLM でシナリオ JSON 生成（タイトル/概要/ロケーション/敵情報/プロットフック）
  2. ロケーション・敵ごとに ComfyUI 画像ジョブをキュー
  3. 画像ジョブをポーリングして完了次第ダウンロード → data/images/ 以下に保存
  4. scenarios / scenario_images テーブルに保存

使い方:
  uv run ai-agents/generate_scenarios.py [オプション]

オプション:
  --count N             生成シナリオ数 (デフォルト: 1)
  --rank RANK           ランク A/B/C/D/E (デフォルト: C)
  --model-url URL       テキスト生成 LLM URL (デフォルト: http://localhost:11430)
  --comfy-url URL       ComfyUI URL (例: http://<ComfyUI-HOST>:8188)
  --comfy-model NAME    使用 checkpoint 名 (省略時は自動検出)
  --db PATH             DB ファイルパス
  --images-dir PATH     画像保存先ディレクトリ
  --no-images           画像生成をスキップ
  --publish-quest       生成シナリオをクエストボードに追加
  --backgrounds         共通背景画像（宿屋/ギルド等）を事前生成
  --scenario-id N       既存シナリオの画像のみ再生成
  --dry-run             DB/ファイルを更新しない
  --status              生成済みシナリオ一覧を表示して終了
"""
import argparse
import json
import os
import random
import re
import sqlite3
import sys
import time
import urllib.error
import urllib.request
from pathlib import Path
from typing import Optional

# ── 定数 ─────────────────────────────────────────────────────────────────────

_PROJECT_ROOT      = Path(__file__).resolve().parent.parent
DEFAULT_DB         = _PROJECT_ROOT / "data" / "omni.db"
DEFAULT_LLM        = "http://localhost:11430"
DEFAULT_IMAGES_DIR = _PROJECT_ROOT / "data" / "images"

RANK_DESC = {
    'E': '冒険者1〜2', 'D': '冒険者2〜3', 'C': '冒険者3〜5',
    'B': '冒険者5〜7', 'A': '冒険者7〜10',
}

# 共通背景: (ラベル, 英語プロンプト) のリスト
COMMON_BACKGROUNDS = [
    ("tavern_interior",
     "medieval fantasy tavern interior, stone walls, wooden tables, warm fireplace light, "
     "candles, rustic atmosphere, high detail, no people, no characters"),
    ("guild_hall",
     "adventurer guild hall reception, notice board with parchment quests, "
     "stone architecture, warm torchlight, medieval fantasy, no people"),
    ("forest_path",
     "dark enchanted forest path, ancient gnarled trees, mysterious fog, "
     "fantasy atmosphere, volumetric light, no characters"),
    ("dungeon_entrance",
     "dungeon stone entrance, carved archway, flickering torches, ominous dark passage, "
     "medieval fantasy, atmospheric, no people"),
    ("village_square",
     "medieval fantasy village square, cobblestone road, market stalls, overcast sky, "
     "half-timbered buildings, warm atmosphere, no characters"),
    ("mountain_pass",
     "mountain pass road, rocky cliffs, pine forests, adventurous landscape, "
     "medieval fantasy, dramatic clouds, no characters"),
    ("ancient_ruins",
     "ancient stone ruins, crumbling columns, overgrown vines, mysterious mist, "
     "fantasy atmosphere, dramatic lighting, no characters"),
    ("castle_courtyard",
     "medieval fantasy castle courtyard, stone walls, iron gate, "
     "overcast sky, atmospheric, imposing architecture, no people"),
]

NEGATIVE_PROMPT = (
    "low quality, blurry, text, watermark, signature, logo, "
    "cartoon, anime, ugly, deformed, distorted, bad anatomy, "
    "duplicate, overexposed, oversaturated"
)

SCENARIO_PROMPT = """\
あなたはSW2.5（ソード・ワールド2.5）の経験豊富なGMです。
{rank}ランク（{rank_desc}レベル向け）の短編シナリオを1本生成してください。

## 出力形式（JSONのみ。マークダウン・コードブロック不要）
{{
  "title": "シナリオタイトル（20文字以内）",
  "summary": "一行要約（50文字以内）",
  "description": "シナリオ概要・GMの導入説明（200〜350文字）",
  "client": "依頼人の名前と簡単な説明（30文字以内）",
  "reward": "報酬（例: 5,000G、宝剣エアラリア）",
  "target": "目標・討伐対象（30文字以内）",
  "level": "推奨レベル（例: 冒険者レベル3〜5）",
  "locations": [
    {{
      "name": "場所名（日本語）",
      "description": "場所の雰囲気・描写テキスト（80〜120文字）",
      "image_prompt": "English stable diffusion prompt: fantasy medieval location, atmospheric, detailed, no characters"
    }}
  ],
  "enemies": [
    {{
      "name": "敵名（日本語）",
      "lv": 数値,
      "hp": 数値,
      "mp": 数値,
      "str": 数値,
      "description": "敵の説明（40文字以内）",
      "image_prompt": "English stable diffusion prompt: fantasy creature/monster, front view, detailed, dark background, no text"
    }}
  ],
  "plot_hooks": ["導入フック1（60文字以内）", "導入フック2"],
  "events": [
    {{"title": "イベント名", "description": "イベント説明（80文字以内）"}}
  ]
}}

## 制約
- ロケーション: 2〜3箇所
- 敵: 1〜3種類（{rank}ランク適切な強さ）
- プロットフック: 2〜3個
- image_prompt は英語のみ、具体的な視覚描写
- 全フィールドを必ず出力すること
"""

# ── HTTP ヘルパー ─────────────────────────────────────────────────────────────

def http_get(url: str, timeout: int = 10) -> bytes:
    with urllib.request.urlopen(url, timeout=timeout) as res:
        return res.read()


def http_post(url: str, payload: dict, timeout: int = 120) -> dict:
    data = json.dumps(payload).encode()
    req = urllib.request.Request(url, data=data,
                                  headers={"Content-Type": "application/json"})
    with urllib.request.urlopen(req, timeout=timeout) as res:
        return json.loads(res.read())


def ping(url: str) -> bool:
    try:
        urllib.request.urlopen(f"{url}/health", timeout=3)
        return True
    except Exception:
        return False


def ping_comfy(url: str) -> bool:
    try:
        urllib.request.urlopen(f"{url}/system_stats", timeout=5)
        return True
    except Exception:
        return False


# ── LLM ──────────────────────────────────────────────────────────────────────

def call_llm(model_url: str, prompt: str, max_tokens: int = 2048,
             timeout: int = 180) -> Optional[str]:
    try:
        result = http_post(f"{model_url}/completion", {
            "prompt": prompt,
            "max_tokens": max_tokens,
            "temperature": 0.85,
            "top_p": 0.95,
            "stop": ["\n\n\n"],
        }, timeout=timeout)
        return result.get("content", "").strip()
    except Exception as e:
        print(f"  [LLM error] {e}", file=sys.stderr)
        return None


def parse_json_response(raw: str) -> Optional[dict]:
    """LLM 出力から最初の { } JSON ブロックを抽出してパース。"""
    raw = re.sub(r"```(?:json)?\s*", "", raw).strip().rstrip("`").strip()
    m = re.search(r"\{.*\}", raw, re.DOTALL)
    if not m:
        return None
    try:
        return json.loads(m.group())
    except json.JSONDecodeError:
        return None


# ── ComfyUI ──────────────────────────────────────────────────────────────────

def comfy_list_models(comfy_url: str) -> list[str]:
    """利用可能な checkpoint モデル一覧を取得。"""
    try:
        data = json.loads(http_get(f"{comfy_url}/object_info/CheckpointLoaderSimple", timeout=10))
        info = data.get("CheckpointLoaderSimple", {})
        required = info.get("input", {}).get("required", {})
        ckpt = required.get("ckpt_name", [[]])
        return ckpt[0] if ckpt else []
    except Exception:
        return []


def build_workflow(positive: str, negative: str, model: str,
                   width: int = 768, height: int = 512, seed: Optional[int] = None) -> dict:
    """標準 txt2img ワークフローを構築。"""
    if seed is None:
        seed = random.randint(1, 2**31)
    return {
        "4": {
            "class_type": "CheckpointLoaderSimple",
            "inputs": {"ckpt_name": model}
        },
        "5": {
            "class_type": "EmptyLatentImage",
            "inputs": {"width": width, "height": height, "batch_size": 1}
        },
        "6": {
            "class_type": "CLIPTextEncode",
            "inputs": {"text": positive, "clip": ["4", 1]}
        },
        "7": {
            "class_type": "CLIPTextEncode",
            "inputs": {"text": negative, "clip": ["4", 1]}
        },
        "3": {
            "class_type": "KSampler",
            "inputs": {
                "seed": seed,
                "steps": 25,
                "cfg": 7.0,
                "sampler_name": "euler_ancestral",
                "scheduler": "karras",
                "denoise": 1.0,
                "model": ["4", 0],
                "positive": ["6", 0],
                "negative": ["7", 0],
                "latent_image": ["5", 0],
            }
        },
        "8": {
            "class_type": "VAEDecode",
            "inputs": {"samples": ["3", 0], "vae": ["4", 2]}
        },
        "9": {
            "class_type": "SaveImage",
            "inputs": {"filename_prefix": "sw25_batch", "images": ["8", 0]}
        }
    }


def comfy_queue(comfy_url: str, workflow: dict) -> Optional[str]:
    """ComfyUI にプロンプトをキューして prompt_id を返す。"""
    client_id = f"sw25_batch_{int(time.time())}"
    try:
        result = http_post(f"{comfy_url}/prompt",
                           {"prompt": workflow, "client_id": client_id}, timeout=15)
        pid = result.get("prompt_id", "")
        if not pid:
            print(f"  [ComfyUI] empty prompt_id", file=sys.stderr)
            return None
        return pid
    except Exception as e:
        print(f"  [ComfyUI queue] {e}", file=sys.stderr)
        return None


def comfy_poll(comfy_url: str, prompt_id: str,
               timeout_sec: int = 180, interval: int = 3) -> Optional[str]:
    """完了するまでポーリングし、生成された画像ファイル名を返す。"""
    deadline = time.time() + timeout_sec
    while time.time() < deadline:
        time.sleep(interval)
        try:
            raw = http_get(f"{comfy_url}/history/{prompt_id}", timeout=10)
            history = json.loads(raw)
        except Exception:
            continue
        entry = history.get(prompt_id)
        if not entry:
            continue
        if not entry.get("status", {}).get("completed"):
            continue
        for node in entry.get("outputs", {}).values():
            for img in node.get("images", []):
                if img.get("filename") and img.get("type") == "output":
                    return img["filename"]
    print(f"  [ComfyUI] timeout waiting for {prompt_id}", file=sys.stderr)
    return None


def comfy_download(comfy_url: str, filename: str, dest_path: Path) -> bool:
    """ComfyUI /view から画像をダウンロードして dest_path に保存。"""
    url = f"{comfy_url}/view?filename={filename}&type=output"
    try:
        data = http_get(url, timeout=30)
        dest_path.parent.mkdir(parents=True, exist_ok=True)
        dest_path.write_bytes(data)
        return True
    except Exception as e:
        print(f"  [ComfyUI download] {e}: {url}", file=sys.stderr)
        return False


# ── DB ────────────────────────────────────────────────────────────────────────

def ensure_tables(con: sqlite3.Connection) -> None:
    con.executescript("""
        CREATE TABLE IF NOT EXISTS scenarios (
            id               INTEGER PRIMARY KEY AUTOINCREMENT,
            title            TEXT    NOT NULL DEFAULT '',
            rank             TEXT    NOT NULL DEFAULT 'C',
            summary          TEXT    NOT NULL DEFAULT '',
            description      TEXT    NOT NULL DEFAULT '',
            client           TEXT    NOT NULL DEFAULT '',
            reward           TEXT    NOT NULL DEFAULT '',
            target           TEXT    NOT NULL DEFAULT '',
            level            TEXT    NOT NULL DEFAULT '',
            locations_json   TEXT    NOT NULL DEFAULT '[]',
            enemies_json     TEXT    NOT NULL DEFAULT '[]',
            plot_hooks_json  TEXT    NOT NULL DEFAULT '[]',
            events_json      TEXT    NOT NULL DEFAULT '[]',
            status           TEXT    NOT NULL DEFAULT 'draft',
            generated_at     DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
        );
        CREATE TABLE IF NOT EXISTS scenario_images (
            id           INTEGER PRIMARY KEY AUTOINCREMENT,
            scenario_id  INTEGER REFERENCES scenarios(id) ON DELETE CASCADE,
            category     TEXT    NOT NULL DEFAULT 'scene',
            label        TEXT    NOT NULL DEFAULT '',
            file_path    TEXT    NOT NULL DEFAULT '',
            prompt_text  TEXT    NOT NULL DEFAULT '',
            width        INTEGER NOT NULL DEFAULT 768,
            height       INTEGER NOT NULL DEFAULT 512,
            generated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
        );
        CREATE INDEX IF NOT EXISTS idx_scenarios_status     ON scenarios(status);
        CREATE INDEX IF NOT EXISTS idx_scenario_images_scen ON scenario_images(scenario_id);
        CREATE INDEX IF NOT EXISTS idx_scenario_images_cat  ON scenario_images(category);
    """)


def insert_scenario(con: sqlite3.Connection, data: dict) -> int:
    cur = con.execute("""
        INSERT INTO scenarios
          (title, rank, summary, description, client, reward, target, level,
           locations_json, enemies_json, plot_hooks_json, events_json, status)
        VALUES (?,?,?,?,?,?,?,?,?,?,?,?,'draft')
    """, (
        data.get("title", "")[:100],
        data.get("rank", "C"),
        data.get("summary", "")[:100],
        data.get("description", ""),
        data.get("client", "")[:100],
        data.get("reward", "")[:100],
        data.get("target", "")[:100],
        data.get("level", "")[:50],
        json.dumps(data.get("locations", []), ensure_ascii=False),
        json.dumps(data.get("enemies", []), ensure_ascii=False),
        json.dumps(data.get("plot_hooks", []), ensure_ascii=False),
        json.dumps(data.get("events", []), ensure_ascii=False),
    ))
    return cur.lastrowid


def insert_image(con: sqlite3.Connection, scenario_id: Optional[int],
                 category: str, label: str, file_path: str,
                 prompt_text: str, width: int, height: int) -> int:
    cur = con.execute("""
        INSERT INTO scenario_images
          (scenario_id, category, label, file_path, prompt_text, width, height)
        VALUES (?,?,?,?,?,?,?)
    """, (scenario_id, category, label, file_path, prompt_text, width, height))
    return cur.lastrowid


def mark_scenario_ready(con: sqlite3.Connection, scenario_id: int) -> None:
    con.execute("UPDATE scenarios SET status='ready' WHERE id=?", (scenario_id,))


def publish_to_quests(con: sqlite3.Connection, scenario_id: int) -> None:
    """シナリオをクエストボードに追加（quests テーブル）。"""
    row = con.execute(
        "SELECT title, rank, description, client, reward, target, level FROM scenarios WHERE id=?",
        (scenario_id,)
    ).fetchone()
    if not row:
        return
    title, rank, desc, client, reward, target, level = row
    # すでに同名クエストがあればスキップ
    exists = con.execute("SELECT id FROM quests WHERE title=?", (title,)).fetchone()
    if exists:
        print(f"  [quest] すでに登録済み: {title!r}", file=sys.stderr)
        return
    con.execute("""
        INSERT INTO quests (rank, title, description, client, reward, target, level, tags, status)
        VALUES (?,?,?,?,?,?,?,'','available')
    """, (rank, title, desc, client, reward, target, level))


def show_status(con: sqlite3.Connection) -> None:
    rows = con.execute("""
        SELECT s.id, s.title, s.rank, s.status, s.generated_at,
               COUNT(si.id) as img_count
        FROM scenarios s
        LEFT JOIN scenario_images si ON si.scenario_id = s.id
        GROUP BY s.id ORDER BY s.id DESC
    """).fetchall()
    bg_count = con.execute(
        "SELECT COUNT(*) FROM scenario_images WHERE scenario_id IS NULL"
    ).fetchone()[0]

    print("\n── シナリオ一覧 ─────────────────────────────────────────")
    print(f"  {'ID':>4}  {'タイトル':<22}  {'ランク'}  {'ステータス':<8}  {'画像'}  生成日時")
    print(f"  {'─'*4}  {'─'*22}  {'─'*4}  {'─'*8}  {'─'*4}  {'─'*16}")
    for sid, title, rank, status, gen_at, img_count in rows:
        title_disp = title[:20] + ('…' if len(title) > 20 else '')
        print(f"  {sid:>4}  {title_disp:<22}  {rank:<4}  {status:<8}  {img_count:>4}枚  {gen_at[:16]}")
    print(f"\n  計 {len(rows)} シナリオ / 共通背景 {bg_count} 枚")
    print("────────────────────────────────────────────────────────\n")


# ── 画像生成タスク ─────────────────────────────────────────────────────────────

class ImageJob:
    def __init__(self, scenario_id: Optional[int], category: str, label: str,
                 prompt: str, width: int, height: int, dest_path: Path):
        self.scenario_id = scenario_id
        self.category    = category
        self.label       = label
        self.prompt      = prompt
        self.width       = width
        self.height      = height
        self.dest_path   = dest_path
        self.prompt_id: Optional[str] = None
        self.done        = False
        self.file_path   = ""


def generate_images(jobs: list[ImageJob], comfy_url: str, comfy_model: str,
                    dry_run: bool) -> None:
    """全画像ジョブをキューしてポーリングでダウンロード。"""
    if not jobs:
        return

    # ── キュー ──
    print(f"\n  [画像] {len(jobs)} 枚をキュー中...")
    for job in jobs:
        if dry_run:
            job.done = True
            job.file_path = str(job.dest_path) + " (dry-run)"
            continue
        w = build_workflow(job.prompt, NEGATIVE_PROMPT, comfy_model,
                           width=job.width, height=job.height)
        pid = comfy_queue(comfy_url, w)
        if pid:
            job.prompt_id = pid
            print(f"    キュー: [{job.category}] {job.label!r} → {pid[:8]}…")
        else:
            print(f"    ✗ キュー失敗: {job.label!r}")

    if dry_run:
        return

    # ── ポーリング + ダウンロード（逐次） ──
    print(f"  [画像] 完了待機中（各最大3分）...")
    for job in jobs:
        if not job.prompt_id:
            continue
        filename = comfy_poll(comfy_url, job.prompt_id, timeout_sec=180)
        if not filename:
            print(f"    ✗ タイムアウト: {job.label!r}")
            continue
        ok = comfy_download(comfy_url, filename, job.dest_path)
        if ok:
            job.done = True
            job.file_path = str(job.dest_path)
            print(f"    ✓ 保存: {job.dest_path.name}")
        else:
            print(f"    ✗ ダウンロード失敗: {job.label!r}")


# ── シナリオ生成 ──────────────────────────────────────────────────────────────

def generate_one_scenario(args, con: sqlite3.Connection,
                          comfy_url: Optional[str], comfy_model: Optional[str],
                          idx: int, total: int) -> bool:
    rank = args.rank.upper()
    rank_desc = RANK_DESC.get(rank, '冒険者3〜5')
    print(f"\n  [{idx}/{total}] {rank}ランク シナリオを生成中...")

    # ── LLM でシナリオ生成 ──
    prompt = SCENARIO_PROMPT.format(rank=rank, rank_desc=rank_desc)
    raw = call_llm(args.model_url, prompt)
    if raw is None:
        print("  → LLM 失敗")
        return False

    data = parse_json_response(raw)
    if data is None:
        print(f"  → JSON 解析失敗: {raw[:120]!r}")
        return False

    # rank をデータに設定
    data["rank"] = rank

    title = data.get("title", "(無題)")
    print(f"  → 「{title}」（{rank}ランク）")
    print(f"     要約: {data.get('summary', '')[:60]}")
    print(f"     ロケーション: {len(data.get('locations', []))}箇所 / 敵: {len(data.get('enemies', []))}種")

    # ── DB 保存（シナリオ本体） ──
    if args.dry_run:
        print("  → (dry-run) DB 保存スキップ")
        scenario_id = -1
    else:
        scenario_id = insert_scenario(con, data)
        con.commit()
        print(f"  → scenario ID={scenario_id}")

    # ── 画像ジョブ構築 ──
    jobs: list[ImageJob] = []
    if comfy_url and comfy_model and not args.no_images:
        images_base = args.images_dir / "scenarios" / str(scenario_id)

        # ロケーション画像（横: 768×512）
        for i, loc in enumerate(data.get("locations", []), 1):
            img_prompt = loc.get("image_prompt", "")
            if not img_prompt:
                continue
            dest = images_base / f"loc_{i:02d}_{loc.get('name', 'location')[:20]}.png"
            jobs.append(ImageJob(
                scenario_id=scenario_id if scenario_id > 0 else None,
                category="scene",
                label=loc.get("name", f"location_{i}"),
                prompt=img_prompt,
                width=768, height=512,
                dest_path=dest,
            ))

        # 敵ポートレート（縦: 512×768）
        for i, enemy in enumerate(data.get("enemies", []), 1):
            img_prompt = enemy.get("image_prompt", "")
            if not img_prompt:
                continue
            dest = images_base / f"enemy_{i:02d}_{enemy.get('name', 'enemy')[:20]}.png"
            jobs.append(ImageJob(
                scenario_id=scenario_id if scenario_id > 0 else None,
                category="portrait",
                label=enemy.get("name", f"enemy_{i}"),
                prompt=img_prompt,
                width=512, height=768,
                dest_path=dest,
            ))

        generate_images(jobs, comfy_url, comfy_model, args.dry_run)

    # ── 画像を DB に記録 ──
    if not args.dry_run:
        for job in jobs:
            if job.done and job.file_path:
                insert_image(con, job.scenario_id, job.category, job.label,
                             job.file_path, job.prompt, job.width, job.height)
        mark_scenario_ready(con, scenario_id)
        if args.publish_quest:
            publish_to_quests(con, scenario_id)
        con.commit()
        done_imgs = sum(1 for j in jobs if j.done)
        print(f"  → 完了: scenario={scenario_id}, 画像={done_imgs}/{len(jobs)}枚保存")
    return True


# ── 共通背景生成 ──────────────────────────────────────────────────────────────

def generate_backgrounds(args, con: sqlite3.Connection,
                         comfy_url: str, comfy_model: str) -> None:
    """宿屋・ギルドなど共通背景を事前生成。scenario_id=NULL で保存。"""
    print(f"\n[共通背景] {len(COMMON_BACKGROUNDS)} 枚を生成します...")
    jobs: list[ImageJob] = []
    bg_base = args.images_dir / "backgrounds"

    for label, img_prompt in COMMON_BACKGROUNDS:
        dest = bg_base / f"{label}.png"
        if dest.exists() and not args.dry_run:
            print(f"  スキップ (既存): {dest.name}")
            continue
        jobs.append(ImageJob(
            scenario_id=None,  # 共通背景
            category="background",
            label=label,
            prompt=img_prompt,
            width=768, height=512,
            dest_path=dest,
        ))

    if not jobs:
        print("  すべて生成済みです。")
        return

    generate_images(jobs, comfy_url, comfy_model, args.dry_run)

    if not args.dry_run:
        for job in jobs:
            if job.done and job.file_path:
                # すでに同ラベルがあれば上書き
                existing = con.execute(
                    "SELECT id FROM scenario_images WHERE scenario_id IS NULL AND label=?",
                    (job.label,)
                ).fetchone()
                if existing:
                    con.execute(
                        "UPDATE scenario_images SET file_path=?, generated_at=CURRENT_TIMESTAMP WHERE id=?",
                        (job.file_path, existing[0])
                    )
                else:
                    insert_image(con, None, "background", job.label,
                                 job.file_path, job.prompt, job.width, job.height)
        con.commit()
        done = sum(1 for j in jobs if j.done)
        print(f"\n[共通背景] {done}/{len(jobs)} 枚保存完了")


# ── 既存シナリオの画像再生成 ─────────────────────────────────────────────────

def regenerate_images_for_scenario(args, con: sqlite3.Connection,
                                   comfy_url: str, comfy_model: str) -> None:
    sid = args.scenario_id
    row = con.execute(
        "SELECT title, rank, locations_json, enemies_json FROM scenarios WHERE id=?",
        (sid,)
    ).fetchone()
    if not row:
        print(f"[error] scenario ID={sid} が見つかりません", file=sys.stderr)
        sys.exit(1)

    title, rank, locs_json, enemies_json = row
    print(f"\n[再生成] ID={sid} 「{title}」({rank}ランク) の画像を再生成します")

    locations = json.loads(locs_json or "[]")
    enemies   = json.loads(enemies_json or "[]")
    images_base = args.images_dir / "scenarios" / str(sid)
    jobs: list[ImageJob] = []

    for i, loc in enumerate(locations, 1):
        img_prompt = loc.get("image_prompt", "")
        if not img_prompt:
            continue
        dest = images_base / f"loc_{i:02d}_{loc.get('name', 'location')[:20]}.png"
        jobs.append(ImageJob(None, "scene", loc.get("name", f"loc_{i}"),
                             img_prompt, 768, 512, dest))

    for i, enemy in enumerate(enemies, 1):
        img_prompt = enemy.get("image_prompt", "")
        if not img_prompt:
            continue
        dest = images_base / f"enemy_{i:02d}_{enemy.get('name', 'enemy')[:20]}.png"
        jobs.append(ImageJob(None, "portrait", enemy.get("name", f"enemy_{i}"),
                             img_prompt, 512, 768, dest))

    for job in jobs:
        job.scenario_id = sid

    generate_images(jobs, comfy_url, comfy_model, args.dry_run)

    if not args.dry_run:
        for job in jobs:
            if job.done and job.file_path:
                insert_image(con, sid, job.category, job.label,
                             job.file_path, job.prompt, job.width, job.height)
        mark_scenario_ready(con, sid)
        con.commit()
        done = sum(1 for j in jobs if j.done)
        print(f"\n[再生成] {done}/{len(jobs)} 枚保存完了")


# ── メイン ────────────────────────────────────────────────────────────────────

def main() -> None:
    parser = argparse.ArgumentParser(description="シナリオと素材をバッチ生成するスクリプト")
    parser.add_argument("--count",         type=int,  default=1,              help="生成シナリオ数 (デフォルト: 1)")
    parser.add_argument("--rank",          default="C",                       help="ランク A/B/C/D/E (デフォルト: C)")
    parser.add_argument("--model-url",     default=DEFAULT_LLM,               help="LLM URL")
    parser.add_argument("--comfy-url",     default=None,                      help="ComfyUI URL (例: http://<ComfyUI-HOST>:8188)")
    parser.add_argument("--comfy-model",   default=None,                      help="使用する checkpoint モデル名")
    parser.add_argument("--db",            type=Path, default=DEFAULT_DB,     help="DB ファイルパス")
    parser.add_argument("--images-dir",    type=Path, default=DEFAULT_IMAGES_DIR, help="画像保存先ディレクトリ")
    parser.add_argument("--no-images",     action="store_true",               help="画像生成をスキップ")
    parser.add_argument("--publish-quest", action="store_true",               help="クエストボードに追加")
    parser.add_argument("--backgrounds",   action="store_true",               help="共通背景を事前生成")
    parser.add_argument("--scenario-id",   type=int,  default=None,           help="既存シナリオの画像のみ再生成")
    parser.add_argument("--dry-run",       action="store_true",               help="DB/ファイルを更新しない")
    parser.add_argument("--status",        action="store_true",               help="生成済みシナリオ一覧を表示")
    args = parser.parse_args()

    # ── DB 接続 ──
    if not args.db.exists():
        print(f"[error] DB が見つかりません: {args.db}", file=sys.stderr)
        sys.exit(1)

    con = sqlite3.connect(args.db)
    con.row_factory = sqlite3.Row
    con.execute("PRAGMA foreign_keys = ON")
    ensure_tables(con)

    if args.status:
        show_status(con)
        con.close()
        return

    # ── サービス確認 ──
    llm_ok   = ping(args.model_url)
    print(f"[check] LLM ({args.model_url}): {'✓ 接続OK' if llm_ok else '✗ 未接続'}")

    comfy_url   = None
    comfy_model = None

    if not args.no_images:
        raw_comfy = args.comfy_url or os.environ.get("COMFY_URL", "")
        if raw_comfy:
            comfy_ok = ping_comfy(raw_comfy)
            print(f"[check] ComfyUI ({raw_comfy}): {'✓ 接続OK' if comfy_ok else '✗ 未接続'}")
            if comfy_ok:
                comfy_url = raw_comfy
                # モデル自動検出
                comfy_model = args.comfy_model
                if not comfy_model:
                    models = comfy_list_models(comfy_url)
                    if models:
                        comfy_model = models[0]
                        print(f"[check] ComfyUI モデル自動選択: {comfy_model}")
                    else:
                        print("[warn]  ComfyUI モデルを取得できませんでした。--comfy-model を指定してください。")
                        comfy_url = None
                else:
                    print(f"[check] ComfyUI モデル: {comfy_model}")
            else:
                print("[warn]  ComfyUI に接続できません。画像生成をスキップします。")
        else:
            print("[check] ComfyUI: 未設定（--comfy-url または COMFY_URL 環境変数）")

    if not llm_ok:
        print("[error] LLM に接続できません。llama-server が起動しているか確認してください。")
        sys.exit(1)

    # ── 共通背景生成 ──
    if args.backgrounds:
        if not comfy_url or not comfy_model:
            print("[error] --backgrounds には ComfyUI 接続が必要です。")
            sys.exit(1)
        generate_backgrounds(args, con, comfy_url, comfy_model)
        con.close()
        return

    # ── 既存シナリオの画像再生成 ──
    if args.scenario_id is not None:
        if not comfy_url or not comfy_model:
            print("[error] --scenario-id には ComfyUI 接続が必要です。")
            sys.exit(1)
        regenerate_images_for_scenario(args, con, comfy_url, comfy_model)
        con.close()
        return

    # ── シナリオバッチ生成 ──
    rank = args.rank.upper()
    if rank not in RANK_DESC:
        print(f"[error] 不正なランク: {rank!r}（A/B/C/D/E のいずれかを指定）", file=sys.stderr)
        sys.exit(1)

    print(f"\n[batch] {args.count} 本のシナリオを生成します（{rank}ランク）"
          + (" (dry-run)" if args.dry_run else "")
          + (" + クエスト投入" if args.publish_quest else "")
          + "\n")

    ok_count = fail_count = 0
    t_start = time.time()

    for i in range(1, args.count + 1):
        ok = generate_one_scenario(args, con, comfy_url, comfy_model, i, args.count)
        if ok:
            ok_count += 1
        else:
            fail_count += 1

    elapsed = time.time() - t_start
    print(f"\n── 結果 ───────────────────────────────────────────────")
    print(f"  成功: {ok_count}本  失敗: {fail_count}本")
    print(f"  経過時間: {elapsed:.1f}秒")
    if not args.dry_run:
        show_status(con)

    con.close()


if __name__ == "__main__":
    main()
