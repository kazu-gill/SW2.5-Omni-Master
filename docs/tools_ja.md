# SW2.5 Omni-Master — ツール使用マニュアル（日本語）

> バージョン: 1.0 / 言語: 日本語

本ドキュメントでは、プロジェクト付属のシェル / PowerShell スクリプトおよび Python ユーティリティの詳細な使い方を説明します。

LLM（チャット・埋め込み）は別リポジトリの推論基盤 mlx-serve（`/Users/kf/AI/MLX`）が提供します。本プロジェクトのスクリプトは LLM プロセスを起動しません。

---

## 目次

1. [setup.sh — 初期構築・リセット](#1-setupsh)
2. [Start-Omni.ps1 — サービス起動](#2-start-omnips1)
3. [Stop-Omni.ps1 — サービス停止](#3-stop-omnips1)
4. [ocr_rulebook.py — ルールブックインポート](#4-ocr_rulebookpy)
5. [embed_chunks.py — 埋め込みベクトル生成](#5-embed_chunkspy)
6. [optimize_chunks.py — チャンク最適化](#6-optimize_chunkspy)
7. [generate_scenarios.py — シナリオバッチ生成](#7-generate_scenariospy)

---

## 1. setup.sh

初回セットアップ、またはセッション間のデータリセットに使用します。

### 概要

```
./setup.sh [オプション]
```

| オプション | 説明 |
|-----------|------|
| （なし） | 通常リセット。ルールブックデータを保持したままゲームデータをクリア |
| `--full` | rag_chunks（ルールブック）も含む全テーブルを削除 |
| `-h`, `--help` | ヘルプを表示 |

### 動作の詳細

**通常リセット（引数なし）**

削除されるもの:
- `sessions`（カスケードで `npc_sheets`, `session_logs`, `checkpoints` も削除）
- `quests`

保持されるもの:
- `rag_chunks`（ルールブックデータ）
- `rag_chunks_opt`（最適化済みチャンク）
- `player_characters`（PCシート）
- `scenarios`, `scenario_images`（事前生成シナリオ）
- `data/personas/*.yaml`（ペルソナ定義）

リセット後、セッション #1 が自動で再作成されます（アプリが `SESSION_ID=1` を前提とするため）。

**フルリセット（--full）**

全テーブルを削除。ルールブックを再インポートする必要があります。初期構築後の初めてのセットアップや、データを完全に作り直すときに使用します。

### スキーマ適用

DB ファイルが存在しない場合は `orchestrator/pkg/db/schema.sql` を適用して新規作成します。既存 DB の場合は `CREATE TABLE IF NOT EXISTS` による idempotent な適用が行われます。また、以下のマイグレーションを自動的に試みます（既に存在する場合はエラーを無視）:

```sql
ALTER TABLE rag_chunks ADD COLUMN tag TEXT NOT NULL DEFAULT '';
ALTER TABLE rag_chunks ADD COLUMN enabled INTEGER NOT NULL DEFAULT 1;
ALTER TABLE rag_chunks ADD COLUMN overrides_id INTEGER REFERENCES rag_chunks(id);
```

### 設定ファイル

`config.env` から `DB_PATH` を読み込みます。未設定の場合は `data/omni.db` を使用。

### 実行例

```bash
# 通常のセッション前リセット
./setup.sh

# ルールブックも含めて完全初期化
./setup.sh --full

# フルリセット後のルールブックインポート例
./setup.sh --full
uv run ai-agents/ocr_rulebook.py data/rulebook/swordworld2.5_rulebook1.md --db data/omni.db
uv run ai-agents/embed_chunks.py --db data/omni.db --embed-url http://127.0.0.1:11455
```

---

## 2. Start-Omni.ps1

Go オーケストレーターと Vite UI をバックグラウンドで起動します（PowerShell 7）。mlx-serve は起動しません。

### 概要

```
pwsh scripts/Start-Omni.ps1 [-Follow]
```

| オプション | 説明 |
|-----------|------|
| `-Follow` | 起動後にログを `tail -f` で表示（Ctrl+C で表示だけ終了し、サービスは動き続ける） |

その他の設定はすべて `config.env` から読み込まれます（OS 環境変数 > config.env > デフォルト値）。

### 起動順序

1. `.pids` が残っていれば終了（二重起動防止。先に `Stop-Omni.ps1` を実行）
2. `LLM_URL` と `EMBED_URL` の `/health` を確認。応答がないプロファイルがあれば、次のような起動コマンドを表示して終了:
   ```
   pwsh /Users/kf/AI/MLX/scripts/Start-MlxServe.ps1 -Profile gemma4-31b-it-heretic-4bit-mtp
   pwsh /Users/kf/AI/MLX/scripts/Start-MlxServe.ps1 -Profile bge-m3-q8
   ```
3. Go オーケストレーターをビルド（`.bin/orchestrator`）して起動（`ADDR`、既定 8080）
4. Vite UI dev server 起動（ポート 5173）
5. 両ポートの待ち受けを最大 30 秒確認し、接続先の一覧を表示

### 前提条件

- PowerShell 7（`pwsh`）
- `config.env` が存在すること（`config.env.example` をコピーして作成）
- mlx-serve で `LLM_MODEL_ID` / `EMBED_MODEL_ID` のプロファイルが起動していること
- `.pids` ファイルが存在しないこと

### config.env の主要な設定値

```bash
# オーケストレーターのリッスンアドレス
ADDR=:8080

# DB パス
DB_PATH=/Users/kf/AI/SW2.5-Omni-Master/data/omni.db

# ComfyUI URL（画像生成。空文字で無効化）
COMFY_URL=http://192.168.1.10:8188

# チャットモデル（GM / NPC / Support 共通、mlx-serve）
LLM_URL=http://127.0.0.1:11441
LLM_MODEL_ID=gemma4-31b-it-heretic-4bit-mtp

# 埋め込み（RAG、mlx-serve）
EMBED_URL=http://127.0.0.1:11455
EMBED_MODEL_ID=bge-m3-q8
```

### ログファイル

| ファイル | サービス |
|--------|--------|
| `logs/orchestrator.log` | Go オーケストレーター |
| `logs/ui.log` | Vite dev server |

LLM の推論ログは mlx-serve 側にあります。

### PID 管理

起動したプロセスの PID は `.pids` ファイルに記録されます。`Stop-Omni.ps1` はこのファイルと実際のポートリスナーの両方を使って確実に停止します。

### 旧スクリプト

`start.sh` / `stop.sh` は旧構成（llama-server を直接起動）用で、非推奨です。今後削除します。

---

## 3. Stop-Omni.ps1

Go オーケストレーターと Vite UI を停止します。mlx-serve のプロファイルは停止しません。

### 概要

```
pwsh scripts/Stop-Omni.ps1
```

引数はありません。

### 動作の詳細

ポート 5173（UI）と `ADDR` のポート（オーケストレーター）を LISTEN しているプロセスを `lsof` で特定し、SIGTERM → 1秒後に SIGKILL（生き残っている場合）の順で停止します。

`.pids` ファイルに記録された PID も追加で確認して停止します。最後に `.pids` ファイルを削除します。

### 注意

mlx-serve も停止する場合は mlx-serve 側のスクリプトを使います。

```bash
pwsh /Users/kf/AI/MLX/scripts/Stop-MlxServe.ps1 -All
```

---

## 4. ocr_rulebook.py

ルールブックのテキスト（PDF / Markdown / テキスト）を `rag_chunks` テーブルにインポートします。

### 概要

```
uv run ai-agents/ocr_rulebook.py <ファイルパス> [オプション]
```

| オプション | デフォルト | 説明 |
|-----------|-----------|------|
| `<ファイルパス>` | （必須） | インポートするファイル（.pdf / .md / .txt） |
| `--source-type` | `rulebook` | `rulebook` / `correction` / `houserule` |
| `--db` | `data/omni.db` | DB ファイルパス |

### ソースタイプと優先度

RAG 検索では優先度の高いチャンクが優先して使用されます。

| ソースタイプ | 優先度 | 用途 |
|-----------|--------|------|
| `houserule` | 20 | 独自ルール（最優先） |
| `correction` | 10 | 公式エラッタ・修正 |
| `rulebook` | 0 | 通常のルールブック |

### 対応フォーマット

**PDF（.pdf）**

`pdfplumber` を使ってテキストを抽出します。テキストが埋め込まれた PDF に対して動作します（スキャン画像 PDF は OCR 未対応）。

```bash
uv run ai-agents/ocr_rulebook.py data/rulebook/rulebook.pdf --db data/omni.db
```

**Markdown（.md）**

`# / ## / ###` の見出しをセクション区切りとしてチャンク分割します。意味のまとまりを保った分割になります。

```bash
uv run ai-agents/ocr_rulebook.py data/rulebook/swordworld2.5_rulebook1.md \
    --db data/omni.db \
    --source-type rulebook
```

**テキスト（.txt）**

500文字ごとにチャンク分割します。

### チャンクサイズ

1チャンク = 500文字（デフォルト）。見出しをまたぐ場合はセクション境界で分割されます。

### インポート後の確認

```bash
sqlite3 data/omni.db \
  "SELECT source_type, count(*) FROM rag_chunks GROUP BY source_type;"
```

インポート直後は `embedding IS NULL` です。RAG 検索を使うには `embed_chunks.py` で埋め込みを生成してください。

### 実行例

```bash
# ルールブック（Markdown）をインポート
uv run ai-agents/ocr_rulebook.py \
    data/rulebook/swordworld2.5_rulebook1.md \
    --db data/omni.db

# ハウスルールをテキストで追加
uv run ai-agents/ocr_rulebook.py \
    data/houserules.txt \
    --source-type houserule \
    --db data/omni.db

# PDF ルールブックをインポート
uv run ai-agents/ocr_rulebook.py \
    data/rulebook/supplement.pdf \
    --db data/omni.db \
    --source-type rulebook
```

---

## 5. embed_chunks.py

`rag_chunks` と `rag_chunks_opt` テーブル内の埋め込みが未生成の行にベクトルを生成します。

### 概要

```
uv run ai-agents/embed_chunks.py [オプション]
```

| オプション | デフォルト | 説明 |
|-----------|-----------|------|
| `--db` | `data/omni.db` | DB ファイルパス |
| `--embed-url` | `http://127.0.0.1:11455` | mlx-serve 埋め込みプロファイルの URL |
| `--embed-model-id` | `bge-m3-q8` | 埋め込みプロファイルの model ID（リクエストの `model`） |
| `--batch` | `32` | 1回のコミットで処理する件数 |
| `--reembed-all` | — | 既存の埋め込みをすべて消して（`rag_chunks` / `rag_chunks_opt`）全件作り直す |

### 動作の詳細

1. `--reembed-all` 指定時は `rag_chunks` と `rag_chunks_opt` の `embedding` をすべて NULL に戻す
2. 各テーブルの `embedding IS NULL` の行を `--batch` 件ずつ取得
3. 各行のテキストを `{embed_url}/v1/embeddings` に `{"model": <embed-model-id>, "input": <text>}` で POST
4. 返ってきたベクトル（float32 配列）を BLOB として `embedding` カラムに保存し、`--batch` 件ごとにコミット
5. 未処理の行がなくなるまで繰り返す（失敗した行は NULL のまま残し、警告を表示）

開始時に `{embed_url}/health` を確認し、応答がなければ `Start-MlxServe.ps1` の起動コマンドを表示して終了します。

### 埋め込みモデルを変更したとき

モデルを変えるとベクトルの次元が変わります（例: nomic-embed の 768 次元 → bge-m3 の 1024 次元）。古いベクトルと混在すると検索できないため、全件を作り直してください。

```bash
uv run ai-agents/embed_chunks.py --db data/omni.db --reembed-all
```

### 状況確認

```bash
sqlite3 data/omni.db \
  "SELECT count(*) FROM rag_chunks WHERE embedding IS NULL;"
sqlite3 data/omni.db \
  "SELECT count(*) FROM rag_chunks_opt WHERE embedding IS NULL;"
```

### 実行例

```bash
# 未埋め込みの行をすべて処理
uv run ai-agents/embed_chunks.py \
    --db data/omni.db \
    --embed-url http://127.0.0.1:11455 \
    --batch 100
```

---

## 6. optimize_chunks.py

`rag_chunks` をローカル LLM で書き直し、OCR エラー修正・ルビ除去・タグ付与・要約生成を行います。最適化結果は `rag_chunks_opt` テーブルに保存され、RAG 検索で優先使用されます。

### 概要

```
uv run ai-agents/optimize_chunks.py [オプション]
```

| オプション | デフォルト | 説明 |
|-----------|-----------|------|
| `--limit` | `20` | 1回の実行で処理する最大チャンク数 |
| `--model-url` | `http://127.0.0.1:11441` | テキスト最適化 LLM（mlx-serve）の URL |
| `--model-id` | `gemma4-31b-it-heretic-4bit-mtp` | テキスト最適化 LLM の model ID |
| `--embed-url` | `http://127.0.0.1:11455` | 埋め込み LLM（mlx-serve）の URL |
| `--embed-model-id` | `bge-m3-q8` | 埋め込み LLM の model ID |
| `--db` | `data/omni.db` | DB ファイルパス |
| `--dry-run` | — | DB/ファイルを更新せず処理内容だけ表示 |
| `--retry-failed` | — | `failed` ステータスのチャンクを再処理 |
| `--status` | — | 処理状況の統計を表示して終了 |
| `--chunk-id N` | — | 指定 ID のチャンクのみ処理（デバッグ用） |

### 処理フロー

```
rag_chunks (原文)
    ↓ LLM に送信（OPTIMIZE_PROMPT、/v1/chat/completions）
    ↓ JSON レスポンス解析
rag_chunks_opt (最適化済みテキスト + tag + summary)
    ↓ embed_chunks.py と同様の処理
rag_chunks_opt.embedding（再埋め込み）
```

### タグ一覧

最適化 LLM が選択する 8 種類のタグ:

| タグ | 内容 |
|-----|------|
| `combat` | 戦闘ルール |
| `magic` | 魔法・呪文 |
| `character` | キャラクター作成・能力値 |
| `item` | 武器・防具・アイテム |
| `world` | 世界観・設定 |
| `general` | 一般ルール |
| `status` | 状態異常・バフ・デバフ |
| `rule` | その他ルール |

### 状況確認

```bash
uv run ai-agents/optimize_chunks.py --status
```

出力例:
```
処理状況:
  完了 (done):      892
  失敗 (failed):     12
  未処理:           386
  合計:            1290
```

### 実行例

```bash
# 状況確認
uv run ai-agents/optimize_chunks.py --status

# 50件バッチ処理（システム未使用時間帯に推奨）
uv run ai-agents/optimize_chunks.py \
    --limit 50 \
    --model-url http://127.0.0.1:11441 \
    --embed-url http://127.0.0.1:11455

# 失敗チャンクを再処理
uv run ai-agents/optimize_chunks.py --retry-failed --limit 20

# 特定チャンクのドライラン（デバッグ）
uv run ai-agents/optimize_chunks.py --chunk-id 42 --dry-run

# 別の mlx-serve プロファイルを使う場合は URL と model ID を指定
uv run ai-agents/optimize_chunks.py \
    --model-url http://127.0.0.1:<port> \
    --model-id <profile> \
    --limit 100
```

### 注意事項

- mlx-serve のチャットモデル（デフォルト 11441）と埋め込みモデル（11455）の両方が稼働している必要があります
- リクエストには必ず `model`（model ID）、ランダムな `seed`、thinking 無効化フラグが付きます（共通モジュール `ai-agents/mlx_client.py`）。model ID が mlx-serve のプロファイルと違うと、mlx-serve 側で常駐モデルが入れ替わります
- ゲームセッション中の実行はパフォーマンスに影響する可能性があります。システム未使用時間帯（深夜など）に実行することを推奨します
- 1チャンクあたり約 3〜10秒かかります（モデルの速度に依存）

---

## 7. generate_scenarios.py

シナリオのテキスト素材と ComfyUI 画像を一括生成してDBに保存します。

### 概要

```
uv run ai-agents/generate_scenarios.py [オプション]
```

| オプション | デフォルト | 説明 |
|-----------|-----------|------|
| `--count N` | `1` | 生成するシナリオ数 |
| `--rank RANK` | `C` | ランク（A/B/C/D/E） |
| `--model-url URL` | `http://127.0.0.1:11441` | テキスト生成 LLM（mlx-serve）の URL |
| `--model-id ID` | `gemma4-31b-it-heretic-4bit-mtp` | テキスト生成 LLM の model ID |
| `--comfy-url URL` | （なし） | ComfyUI の URL。省略すると画像生成なし |
| `--comfy-model NAME` | （自動検出） | 使用 checkpoint モデル名 |
| `--db PATH` | `data/omni.db` | DB ファイルパス |
| `--images-dir PATH` | `data/images` | 画像保存先ディレクトリ |
| `--no-images` | — | 画像生成をスキップ |
| `--publish-quest` | — | 生成シナリオをクエストボードに追加 |
| `--backgrounds` | — | 共通背景画像のみ生成（シナリオ生成なし） |
| `--scenario-id N` | — | 既存シナリオの画像のみ再生成 |
| `--dry-run` | — | DB/ファイルを更新しない |
| `--status` | — | 生成済みシナリオ一覧を表示して終了 |

### ランク別レベル帯

| ランク | 対象レベル |
|-------|-----------|
| E | 冒険者 1〜2 |
| D | 冒険者 2〜3 |
| C | 冒険者 3〜5 |
| B | 冒険者 5〜7 |
| A | 冒険者 7〜10 |

### 生成されるコンテンツ

1つのシナリオにつき以下が生成されます:

| カテゴリ | 内容 | 画像サイズ |
|---------|------|-----------|
| `scene` | ロケーション背景画像 | 768×512（横） |
| `portrait` | 敵/NPC ポートレート | 512×768（縦） |

テキスト情報:
- タイトル・ランク・概要・説明
- 依頼人情報・報酬・目標
- ロケーション一覧（名前・説明・画像プロンプト）
- 敵情報（名前・説明・ステータス・画像プロンプト）
- プロットフック・イベント

### 共通背景画像

8種類の汎用背景を事前生成して複数シナリオで流用します:

| ラベル | 内容 |
|-------|------|
| `tavern_interior` | 宿屋の内装 |
| `guild_hall` | 冒険者ギルドホール |
| `forest_path` | 森の小道 |
| `dungeon_entrance` | ダンジョン入口 |
| `village_square` | 村の広場 |
| `mountain_pass` | 山道 |
| `ancient_ruins` | 古代遺跡 |
| `castle_courtyard` | 城の中庭 |

保存先: `data/images/backgrounds/`

DB では `scenario_images.scenario_id = NULL` で識別されます。

### ComfyUI 画像生成の仕組み

1. ComfyUI の `/object_info/CheckpointLoaderSimple` からモデル一覧を取得
2. 使用モデルを自動選択（または `--comfy-model` で手動指定）
3. 7ノード構成の txt2img ワークフローを生成:
   - CheckpointLoaderSimple → CLIPTextEncode ×2 → EmptyLatentImage → KSampler → VAEDecode → SaveImage
   - KSampler: 25ステップ、Euler Ancestral、Karras スケジューラ、CFG 7.5
4. `/prompt` にキューイング → `prompt_id` 取得
5. `/history/{prompt_id}` を 2秒ごとにポーリング（最大 120秒）
6. 完了後 `/view?filename=...&type=output` からダウンロード

### データベース保存

```
scenarios テーブル:
  id, title, rank, summary, description, client, reward,
  target, level, locations_json, enemies_json, plot_hooks_json,
  events_json, status, generated_at

scenario_images テーブル:
  id, scenario_id (*NULL=共通背景), category, label,
  file_path, width, height, generated_at
```

### 実行例

```bash
# 基本: Cランクシナリオを1本生成（テキストのみ）
uv run ai-agents/generate_scenarios.py

# 3本生成してクエストに登録
uv run ai-agents/generate_scenarios.py \
    --count 3 \
    --rank C \
    --no-images \
    --publish-quest

# 画像付きで2本生成
uv run ai-agents/generate_scenarios.py \
    --count 2 \
    --rank B \
    --comfy-url http://192.168.1.10:8188 \
    --publish-quest

# モデルを手動指定
uv run ai-agents/generate_scenarios.py \
    --comfy-url http://192.168.1.10:8188 \
    --comfy-model "dreamshaper_8.safetensors" \
    --count 1

# 共通背景を事前生成
uv run ai-agents/generate_scenarios.py \
    --backgrounds \
    --comfy-url http://192.168.1.10:8188

# 既存シナリオ（ID=3）に画像を後から追加
uv run ai-agents/generate_scenarios.py \
    --scenario-id 3 \
    --comfy-url http://192.168.1.10:8188

# 生成状況を確認
uv run ai-agents/generate_scenarios.py --status

# DB を更新せずに動作確認
uv run ai-agents/generate_scenarios.py \
    --count 1 \
    --dry-run
```

### cronジョブによる定期実行例

```bash
# crontab -e に追加
# 毎日午前2時に Cランクシナリオを2本自動生成
0 2 * * * cd /Users/kf/AI/SW2.5-Omni-Master && \
    uv run ai-agents/generate_scenarios.py \
    --count 2 --rank C --no-images --publish-quest \
    >> logs/generate_scenarios.log 2>&1
```

### 注意事項

- mlx-serve のチャットモデル（デフォルト 11441、`/v1/chat/completions`）が稼働している必要があります（テキスト生成に使用）
- ComfyUI を使用する場合、Windows 側の ComfyUI が起動していることを確認してください
- `--no-images` を付けると ComfyUI なしで実行できます
- `--publish-quest` で生成されたシナリオは UI の QUEST BOARD に即座に表示されます

---

## 依存関係の確認

```bash
# Python 依存関係のインストール確認
cd ai-agents
uv pip list

# uv がインストールされていない場合
pip install uv

# 全依存関係をインストール
uv sync
```

`ai-agents/pyproject.toml` に記載された依存関係が `uv run` によって自動管理されます。

---

## config.env テンプレート

`config.env.example` をコピーして作成します（`cp config.env.example config.env`）。主なキー:

```bash
# API サーバー
ADDR=:8080
DB_PATH=/Users/kf/AI/SW2.5-Omni-Master/data/omni.db

# ComfyUI（空文字で無効化）
COMFY_URL=http://192.168.1.10:8188

# チャットモデル（mlx-serve、GM / NPC / Support 共通）
LLM_URL=http://127.0.0.1:11441
LLM_MODEL_ID=gemma4-31b-it-heretic-4bit-mtp
LLM_TIMEOUT_SEC=300
STAGE_RETRY_LIMIT=2

# 埋め込み（mlx-serve、RAG）
EMBED_URL=http://127.0.0.1:11455
EMBED_MODEL_ID=bge-m3-q8
```
