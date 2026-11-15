# SW2.5 Omni-Master

![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)
![Go](https://img.shields.io/badge/Go-1.26-00ADD8?logo=go)
![React](https://img.shields.io/badge/React-19-61DAFB?logo=react)
![Python](https://img.shields.io/badge/Python-3.14%2B-3776AB?logo=python)

**ソード・ワールド 2.5** 向け、ローカル LLM による完全自律型 AI-TRPG オーケストレーションシステム。

クラウド API を一切使わず、ローカルで動作する 1 つの LLM が GM・NPC・リソース検証を順番に担当し、ルール参照 (RAG) と組み合わせてゲームを自動処理します。LLM は別リポジトリの推論基盤 mlx-serve (`~/AI/MLX`) が提供し、Omni-Master 自身は推論プロセスを起動しません。プレイヤーは Web UI から行動を入力するだけで、AI が即座にナラティブを生成してゲームを進行します。

---

## 特徴 (Features)

| 機能 | 概要 |
|------|------|
| **自律 GM エージェント** | Gemma 4 31B-it がナラティブとルーリングを生成 |
| **マルチ NPC エージェント** | 最大 3 体の NPC が同じモデルを順番に使い、ペルソナ (性格・口調・経歴・行動原則) に沿って行動。段ごとに出力を検証し、不正なら再思考 |
| **セッションの記憶** | 卓の約束・受注クエスト・PC・NPC ペルソナを固定の前置きにし、3 ターンごとに書くあらすじと、まだあらすじにしていないターンの原文で前の出来事を覚えておく |
| **判定はコードが振る** | GM / NPC は判定の種類・基準値・目標値だけを書き、2d6 はオーケストレーターが振る (12 は自動成功、2 は自動失敗) |
| **ルールブック RAG** | ベクター検索に加え、特技・魔法などの用語を読みで引く (「庇う」「かばう」でも同じ用語に当たる)。ハウスルール・訂正を優先 |
| **複数セッション** | セッションの作成・一覧・削除。中断、直前のターンの作り直し (入力の修正も可)、任意のターンの開始前への巻き戻し (NPC の HP/MP・配置も戻る。チェックポイントは最新 10 件) |
| **リアルタイム UI** | WebSocket で段ごとの進行と GM / NPC の本文を逐次表示。ダイスとルールのチップ、3 レーンの配置、スマホ幅 (375px) とダークモードに対応 |
| **シーン画像** | MLX の画像工房 (mlx-studio-web) で 1 ターン目と数ターンごとに挿絵を生成 (オプション) |
| **バッチメンテナンス** | ルールブック Markdown のチャンク化・インポート、シナリオのオフライン事前生成 |

---

## システム構成 (Architecture)

```
ブラウザ (React UI)  :5173
        │ HTTP / WebSocket
        ▼
Go Orchestrator  :8080
  ├─ REST API ハンドラ
  ├─ ゲームエンジン（ターン処理）
  └─ WebSocket ハブ（ブロードキャスト）
        │ HTTP (OpenAI 互換、SSE ストリーミング、X-APC-Tenant: sw25)
        ├─▶ mlx-serve  :11441  gemma4-31b-it-heretic-4bit-mtp
        │                      (GM / NPC / Support / 要約を 1 モデルで直列処理)
        ├─▶ mlx-serve  :11455  bge-m3-q8 (RAG 埋め込み, 1024 次元)
        └─▶ mlx-studio-web :11462  画像工房 (オプション。構図は Gemma、描画は ComfyUI)
                │
        SQLite (omni.db)
          sessions / npc_sheets / turn_results / session_summaries / rag_chunks / checkpoints …
```

**ターン処理フロー:**

すべての LLM 段は同じモデルを 1 段ずつ直列に呼びます。1 セッションで同時に走るターンは 1 つです。

1. プレイヤーが UI から行動を入力 (`POST /api/sessions/{id}/turn`。すぐ 202 が返り、ターン番号はサーバーが決める)
2. Orchestrator がチェックポイント (ターン開始前の NPC の状態) を保存
3. RAG でプレイヤー入力に関連するルールを検索 (上位 5 件)
4. GM が固定の前置き・あらすじ・未要約のターン・ルールを受けてナラティブを生成。判定が要るときは「判定: 誰/判定名/基準値N/目標値M」の行を書き、コードが 2d6 を振る
5. NPC 1 → NPC 2 → NPC 3 の順 (シート順) にアクションを生成。YAML の `check` で判定を求めればコードが振る
6. Support が NPC の行動から引いたルール (上位 3 件) と判定結果を参照してリソース変更 (HP/MP) を確定
7. DB (HP/MP・隊列・ログ・ターン結果) を更新
8. 進行 (段の開始・本文の差分・ルール・判定・確定したターン) を WebSocket `/ws?session_id=` で配信
9. 3 ターンたまるごとにあらすじを更新し、1 ターン目と `IMAGE_EVERY` ターンごとに画像工房でシーン画像を生成 (有効時)

GM 直訴 (`mode: "gm"`) は GM だけが答え、NPC と数値は動きません。各段の出力は検証され、NG ならその段だけを「再思考」させます (却下した出力とエラー内容を渡し、thinking を有効化して呼び直す。最大 `STAGE_RETRY_LIMIT` 回)。それでも NG の場合、NPC はそのターンを見送り、Support は NPC 自身の HP/MP 値を使います (どちらも `warnings` に記録)。GM が NG のままならターンは失敗します。失敗・中断・`TURN_TIMEOUT_SEC` 超過のターンは、開始前の状態に戻ります。段ごとの呼び出し回数はターン結果の `stage_attempts` に入ります。

API の取り決め (エンドポイント・WebSocket のイベント・型) は [docs/api.md](docs/api.md)、処理の詳細は [docs/spec_ja.md](docs/spec_ja.md) を参照してください。

---

## 前提条件 (Prerequisites)

| ツール | バージョン | 用途 |
|--------|-----------|------|
| Mac (Apple Silicon) | — | mlx-serve の実行環境 (想定: Mac mini M5 Pro / 64GB) |
| mlx-serve (`~/AI/MLX`) | — | LLM 推論基盤 (チャット・埋め込み)。使い方は同リポジトリの README を参照 |
| [Go](https://go.dev/) | `mise.toml` で管理 ([mise](https://mise.jdx.dev/)) | バックエンドのビルド・実行 |
| [Node.js](https://nodejs.org/) | `ui/package.json` の `volta` で管理 ([Volta](https://volta.sh/)) | フロントエンドの開発サーバー・ビルド |
| [Python](https://www.python.org/) | 3.14+ | バッチツール |
| [uv](https://github.com/astral-sh/uv) | 最新版 | Python パッケージ管理 |
| [PowerShell](https://github.com/PowerShell/PowerShell) | 7+ | 起動・停止スクリプト |
| SQLite3 | 3.35+ | DB CLI (初期化スクリプト用) |
| mlx-studio-web (MLX の画像工房) | — | シーン画像 **(オプション)**。描画は Windows 側の ComfyUI |

**必要な mlx-serve プロファイル:**

| 用途 | プロファイル | エンドポイント | 備考 |
|------|------------|--------------|------|
| GM / NPC / Support | `gemma4-31b-it-heretic-4bit-mtp` | `http://127.0.0.1:11441` | mlx-vlm + MTP 投機的デコード、約 22GB |
| RAG 埋め込み | `bge-m3-q8` | `http://127.0.0.1:11455` | llama-server (mlx-serve 内部で使用)、1024 次元 |

起動は mlx-serve 側で行います。MLX ダッシュボード (http://127.0.0.1:11470/) から起動するか、スクリプトを使います。チャット用の LLM は同時に 1 本しか常駐できないので、ほかのチャットモデルが動いていれば Gemma に切り替えてください。

```bash
pwsh ~/AI/MLX/scripts/Start-MlxServe.ps1 -Profile gemma4-31b-it-heretic-4bit-mtp
pwsh ~/AI/MLX/scripts/Start-MlxServe.ps1 -Profile bge-m3-q8
```

Gemma プロファイルの起動環境には、プレフィックスキャッシュ (APC) 用に `APC_CHECKPOINT_GUARD_TOKENS=8` と `APC_CHECKPOINT_ENTRIES=4` を設定することを推奨します (理由と設定方法は [docs/mlx-serve-client-notes.md](docs/mlx-serve-client-notes.md))。

シーン画像を使う場合は、MLX の画像工房 (mlx-studio-web, :11462) も起動しておきます。構図を決めるのに Gemma を使うため、Gemma も起動している必要があります。

---

## セットアップ (Setup)

### 1. リポジトリのクローン

```bash
git clone <repo-url> sw25-omni-master
cd sw25-omni-master
```

### 2. 設定ファイルの準備

```bash
cp config.env.example config.env
```

`config.env` を編集します。`DB_PATH` は絶対パスで書きます。LLM の設定は既定値のままで上記 2 プロファイルに接続します:

```bash
DB_PATH=/path/to/sw25-omni-master/data/omni.db

# mlx-serve (既定値。プロファイルを変える場合のみ編集)
LLM_URL=http://127.0.0.1:11441
LLM_MODEL_ID=gemma4-31b-it-heretic-4bit-mtp
EMBED_URL=http://127.0.0.1:11455
EMBED_MODEL_ID=bge-m3-q8

# シーン画像 (MLX の画像工房)。使わない場合は IMAGE_EVERY=0
IMAGES_URL=http://127.0.0.1:11462
IMAGE_EVERY=3
IMAGES_DIR=/path/to/sw25-omni-master/data/images
```

設定キーの詳細は「[設定リファレンス](#設定リファレンス-configuration)」を参照してください。

### 3. データベースの初期化

```bash
./setup.sh
```

> **フルリセット** (ルールデータを含めて全消去) する場合:
> ```bash
> ./setup.sh --full
> ```
>

### 4. ルールブックのインポート (推奨)

```bash
# ルールブック Markdown (data/rulebook/swordworld2.5_rulebook1.md) をチャンク化して確認する (DB は変更しない)
uv run ai-agents/build_rulebook.py

# 確認できたら DB のルールブック行を置き換える (実行前に data/backup/ へバックアップ)
uv run ai-agents/build_rulebook.py --apply

# 埋め込みベクターを生成する (mlx-serve の bge-m3-q8 が起動している必要あり)
uv run ai-agents/embed_chunks.py
```

> 付属のサンプルシナリオ本文は無効 (`enabled=0`) の状態で取り込まれます。使う場合は UI のルール画面で有効にしてください。
>
> 埋め込みモデルを変更した場合はベクトルの次元が変わるため、`--reembed-all` で全チャンクを再埋め込みしてください。

### 5. フロントエンド依存関係のインストール

```bash
cd ui && npm install && cd ..
```

---

## 起動 / 停止 (Start / Stop)

### 起動

先に mlx-serve の 2 プロファイルを起動しておきます ([前提条件](#前提条件-prerequisites) 参照)。

```bash
pwsh scripts/Start-Omni.ps1           # ログを追従する場合は -Follow
```

`Start-Omni.ps1` は `LLM_URL` / `EMBED_URL` の `/health` を確認し、応答がなければ起動すべき `Start-MlxServe.ps1` コマンドを表示して終了します (mlx-serve を自動で起動することはありません)。応答があれば Go Orchestrator をビルドして起動し、続けて Vite Dev Server を起動します。

起動後のアクセス先:

| サービス | URL |
|---------|-----|
| Web UI | http://localhost:5173 |
| REST API | http://localhost:8080 |
| ログ | `logs/orchestrator.log` / `logs/ui.log` |

PID は `.pids` に記録されます。

### 停止

```bash
pwsh scripts/Stop-Omni.ps1
```

Go Orchestrator と Vite Dev Server を停止します。mlx-serve のプロファイルは停止しません。

---

## 設定リファレンス (Configuration)

`config.env` で管理するすべての設定キー:

| キー | デフォルト | 説明 |
|-----|-----------|------|
| `DB_PATH` | `data/omni.db` | SQLite データベースのパス |
| `ADDR` | `:8080` | Go サーバーのリッスンアドレス |
| `LLM_URL` | `http://127.0.0.1:11441` | チャットモデル (mlx-serve) のエンドポイント。GM / NPC / Support / 要約共通 |
| `LLM_MODEL_ID` | `gemma4-31b-it-heretic-4bit-mtp` | 全リクエストに付ける `model`。mlx-serve のプロファイル名と一致させる |
| `LLM_TIMEOUT_SEC` | `300` | LLM 1 リクエストのタイムアウト (秒) |
| `STAGE_RETRY_LIMIT` | `2` | 検証 NG の段を再思考させる回数 (0 で再試行なし) |
| `TURN_TIMEOUT_SEC` | `900` | 1 ターン全体 (全段・再思考込み) の上限 (秒)。超えたターンは開始前に戻る |
| `APC_TENANT` | `sw25` | `X-APC-Tenant` ヘッダーで送る名前。mlx-vlm のプレフィックスキャッシュを他のアプリと分ける |
| `IMAGES_URL` | `http://127.0.0.1:11462` | MLX の画像工房 (mlx-studio-web) のエンドポイント |
| `IMAGE_EVERY` | `3` | 1 ターン目と、以降このターン数ごとにシーン画像を描く。`0` で画像なし |
| `IMAGES_DIR` | `data/images` | シーン画像の保存先 (`sessions/<id>/` 以下) |
| `COMFY_URL` | *(空)* | バッチツール `generate_scenarios.py` 用の ComfyUI。ゲーム本体は使わない |
| `EMBED_URL` | `http://127.0.0.1:11455` | 埋め込みモデル (mlx-serve) のエンドポイント |
| `EMBED_MODEL_ID` | `bge-m3-q8` | 埋め込みリクエストに付ける `model` |
| `PERSONAS_DIR` | `data/personas` | NPC ペルソナ YAML ファイルのディレクトリ |

> `LLM_MODEL_ID` / `EMBED_MODEL_ID` は mlx-serve のプロファイルの `model_id` と一致させてください。mlx-vlm は `model` がないと 422 を返し、別の名前を送ると常駐モデルを入れ替えて再ロードします。
>
> 空の値は「未設定」として扱われ、既定値が使われます。画像を止めるときは `IMAGES_URL` を空にするのではなく `IMAGE_EVERY=0` にしてください。

---

## バッチツール (Batch Tools)

`ai-agents/` 以下の Python スクリプトはオフラインメンテナンス用です。LLM は mlx-serve の OpenAI 互換 API (`/v1/chat/completions`・`/v1/embeddings`) で呼び出します (共通モジュール `ai-agents/mlx_client.py`)。

### ルールブックのインポート

```bash
uv run ai-agents/build_rulebook.py [--md PATH] [--db PATH] [--source rulebook1] [--skip-pages 1-4,242-245] [--preview PATH] [--apply]
```

- OCR 済みルールブック Markdown を見出し・段落単位でチャンク化 (文の途中で切らない)。ルビ・ページ番号・目次・レイアウト要素・記入用シートなどのノイズを除き、各チャンクに「部 > 章 > 見出し」と本のページを付ける
- 既定は確認のみ (`logs/rulebook-preview.jsonl` を書き出す)。`--apply` で DB をバックアップしてからルールブック行を置き換える (ハウスルール・訂正は残す)
- 実装は `ai-agents/rulebook_chunker.py`

### ルールブック原本の OCR 誤字修正

```bash
uv run ai-agents/fix_rulebook_ocr.py            # 何が直るかを表示するだけ
uv run ai-agents/fix_rulebook_ocr.py --apply    # 原本をバックアップしてから書き換える
```

- 原本 Markdown (`data/rulebook/`) の OCR 誤字を置換表で直す。原本を取り直したら、インポートの前に実行する
- 件数が想定と違えば何も書き込まずに止まる。適用済みなら何もしない
- 誤字の候補探しは `uv run ai-agents/find_ocr_suspects.py`、判断の記録は [docs/ocr-suspects-20260927.md](docs/ocr-suspects-20260927.md)

### ハウスルール・訂正テキストの追加

```bash
uv run ai-agents/ocr_rulebook.py <file> [--source-type correction|houserule] [--db PATH]
```

- 短いハウスルール集・訂正集 (`.pdf` / `.md` / `.txt`) を 500 文字単位で取り込む。ルールブック本体には使わない
- `--source-type` で優先度を制御 (`houserule`=20 → `correction`=10 → `rulebook`=0)

### 埋め込みベクターの生成

```bash
uv run ai-agents/embed_chunks.py [--db PATH] [--embed-url URL] [--embed-model-id ID] [--batch 32] [--reembed-all]
```

- `rag_chunks` / `rag_chunks_opt` テーブルの未埋め込み行をすべて処理 (`--batch` 件ごとにコミット)
- `rag_chunks` は「section + 改行 + 本文」を埋め込む。埋め込みサーバが入力過大で拒否した場合は本文を短くして再試行
- `--reembed-all`: 既存の埋め込みを消して全件作り直す (埋め込みモデルを変えたとき)

### チャンクの最適化 (任意・旧方式)

```bash
uv run ai-agents/optimize_chunks.py [--db PATH] [--model-url URL] [--model-id ID] [--embed-url URL] [--embed-model-id ID]
```

- LLM でチャンクテキストを書き換えて `rag_chunks_opt` に保存する。`build_rulebook.py` がルビ・ノイズを除去するため、ルールブックには通常不要

### シナリオのバッチ生成

```bash
uv run ai-agents/generate_scenarios.py [--count N] [--rank A-E] [--model-url URL] [--model-id ID] [--comfy-url URL] [--db PATH]
```

- LLM でシナリオ JSON を生成し、ComfyUI で画像を生成して DB に保存

既定の接続先は `--model-url http://127.0.0.1:11441` / `--model-id gemma4-31b-it-heretic-4bit-mtp`、`--embed-url http://127.0.0.1:11455` / `--embed-model-id bge-m3-q8` です。

---

## 開発 (Development)

### Go バックエンド

```bash
cd orchestrator

# ビルド
go build -o ../.bin/orchestrator ./cmd/server/main.go

# テスト
go test ./...
```

主要パッケージ:

| パス | 役割 |
|------|------|
| `orchestrator/cmd/server/main.go` | エントリーポイント |
| `orchestrator/pkg/api/handler.go` | HTTP / WebSocket ルーター |
| `orchestrator/pkg/api/sessions.go` | セッション API、ターンの実行 (排他・中断・やり直し・巻き戻し) |
| `orchestrator/pkg/game/turn.go` | ターン処理ロジック |
| `orchestrator/pkg/game/context.go` | 固定の前置き (卓の約束・クエスト・PC・NPC ペルソナ) と直近ターン |
| `orchestrator/pkg/game/summary.go` | セッションのあらすじ |
| `orchestrator/pkg/game/checks.go` | 判定 (2d6) |
| `orchestrator/pkg/game/stage.go` | 段ごとの呼び出し・検証・再思考 |
| `orchestrator/pkg/game/validate.go` | GM / NPC / Support 出力の検証 |
| `orchestrator/pkg/llm/client.go` | mlx-serve (OpenAI 互換) クライアント |
| `orchestrator/pkg/rag/engine.go` | ルール検索 (ベクター検索 + 用語インデックス `terms.go`) |
| `orchestrator/pkg/images/studio.go` | MLX 画像工房のクライアント |
| `orchestrator/pkg/db/schema.sql` | SQLite スキーマ |

### React フロントエンド

```bash
cd ui

npm run dev      # Vite 開発サーバー (HMR 付き)
npm run build    # プロダクションビルド
npm run lint     # ESLint チェック

OMNI_API=host:port npx vite   # 別のオーケストレーターに向ける (既定 localhost:8080)
```

開発サーバーで `?mock=1` を付けて開くと、オーケストレーターなしで作り物のターンを再生します。

主要ディレクトリ:

| パス | 役割 |
|------|------|
| `ui/src/views/` | 開始・セッション (`session/`)・クエスト・パーティ (`party/`)・ルール (`rules/`) の画面 |
| `ui/src/components/` | ヘッダー・モーダル・HP/MP バー等の共通部品 |
| `ui/src/hooks/useSessionGame.ts` / `useSessionSocket.ts` | セッションの状態と WebSocket |
| `ui/src/live/turnReducer.ts` | 進行中のターンの表示 (段・本文・判定) |
| `ui/src/styles/` | MLX の画面に揃えたテーマ (`theme.css` / `ui.css`) |
| `ui/src/dev/` | `?mock=1` の作り物の API |
| `ui/src/types.ts` | TypeScript 型定義 |

### Python バッチツール

```bash
cd ai-agents

# 依存関係のインストール
uv sync

# 個別スクリプトの実行
uv run <script>.py --help

# テスト (tests/ 以下。pytest は dev 依存)
uv run pytest
```

---

## ドキュメント (Documentation)

| ファイル | 内容 |
|---------|------|
| [docs/api.md](docs/api.md) | オーケストレーター API の取り決め (UI との契約) |
| [docs/spec_ja.md](docs/spec_ja.md) | システム仕様書 (日本語) |
| [docs/spec_en.md](docs/spec_en.md) | System Specification (English) |
| [docs/maintenance_ja.md](docs/maintenance_ja.md) | 運用手順書 (日本語) |
| [docs/maintenance_en.md](docs/maintenance_en.md) | Maintenance Guide (English) |
| [docs/tools_ja.md](docs/tools_ja.md) | バッチツールリファレンス (日本語) |
| [docs/tools_en.md](docs/tools_en.md) | Batch Tools Reference (English) |
| [docs/plan-retune-20261112.md](docs/plan-retune-20261112.md) | MLX 基盤に合わせた再チューニングと UI 改修の計画 |
| [docs/mlx-serve-client-notes.md](docs/mlx-serve-client-notes.md) | mlx-serve クライアント検証メモ |
| [docs/verification-serial-20260927.md](docs/verification-serial-20260927.md) | 単一モデル直列化の通し検証結果 |
| [docs/verification-apc-20261005.md](docs/verification-apc-20261005.md) | 1 ターン 1 会話化とプレフィックスキャッシュ (APC) の計測 |
| [docs/rulebook-rebuild-20260927.md](docs/rulebook-rebuild-20260927.md) | ルールブック RAG データの作り直し記録 |
| [docs/ocr-suspects-20260927.md](docs/ocr-suspects-20260927.md) | ルールブック OCR 誤字の候補一覧 |

---

## ライセンス (License)

[MIT License](LICENSE) © 2026 kazuki_fujimura
