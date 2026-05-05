# SW2.5 Omni-Master

![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)
![Go](https://img.shields.io/badge/Go-1.26-00ADD8?logo=go)
![React](https://img.shields.io/badge/React-19-61DAFB?logo=react)
![Python](https://img.shields.io/badge/Python-3.14%2B-3776AB?logo=python)

**ソード・ワールド 2.5** 向け、ローカル LLM による完全自律型 AI-TRPG オーケストレーションシステム。

クラウド API を一切使わず、ローカルで動作する複数の LLM インスタンスが GM・NPC・ルール参照・リソース検証を自動処理します。プレイヤーは Web UI から行動を入力するだけで、AI が即座にナラティブを生成してゲームを進行します。

---

## 特徴 (Features)

| 機能 | 概要 |
|------|------|
| **自律 GM エージェント** | Gemma-4B ベースの LLM がリアルタイムでナラティブとルーリングを生成 |
| **マルチ NPC エージェント** | 最大 3 体の NPC が個別の LLM インスタンスで独立して行動 |
| **ルールブック RAG** | ベクター検索でルール文書を動的に参照、一貫性のある判定を実現 |
| **リアルタイム UI** | WebSocket + React によるバトルマップと GM チャンネル |
| **画像生成** | ComfyUI (Stable Diffusion) によるシーン画像・NPC ポートレートの自動生成 (オプション) |
| **セッションチェックポイント** | 最大 10 スナップショットの自動保存と任意時点への復元 |
| **バッチメンテナンス** | ルールブックの OCR インポート、シナリオのオフライン事前生成 |

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
        │ HTTP
        ├─▶ GM LLM          :11430  (Gemma-4B / 高品質モデル)
        ├─▶ Support LLM     :11431  (Gemma-2B / リソース検証)
        ├─▶ NPC-A LLM       :11432  (Gemma-2B)
        ├─▶ NPC-B LLM       :11433  (Gemma-2B)
        ├─▶ NPC-C LLM       :11434  (Gemma-2B)
        └─▶ Embed LLM       :11435  (nomic-embed-text-v1.5)
                │
        SQLite (omni.db)
          sessions / npc_sheets / rag_chunks / checkpoints …
                │ オプション
        ComfyUI  :8188  (別マシン可)
```

**ターン処理フロー:**

1. プレイヤーが UI から行動を入力
2. Orchestrator がチェックポイントを保存
3. GM LLM が RAG コンテキスト付きでナラティブを生成
4. NPC LLM 3 体が並行してアクションを生成
5. Support LLM がリソース変更 (HP/MP) を検証
6. ターン結果を WebSocket で全クライアントへブロードキャスト
7. ComfyUI が非同期でシーン画像を生成 (有効時)

---

## 前提条件 (Prerequisites)

| ツール | バージョン | 用途 |
|--------|-----------|------|
| [Go](https://go.dev/) | 1.26.1+ | バックエンドのビルド・実行 |
| [Node.js](https://nodejs.org/) | 18+ | フロントエンドの開発サーバー・ビルド |
| [Python](https://www.python.org/) | 3.14+ | バッチツール |
| [uv](https://github.com/astral-sh/uv) | 最新版 | Python パッケージ管理 |
| [llama.cpp](https://github.com/ggerganov/llama.cpp) | 最新版 | `llama-server` バイナリ |
| SQLite3 | 3.35+ | DB CLI (初期化スクリプト用) |
| [ComfyUI](https://github.com/comfyanonymous/ComfyUI) | — | 画像生成 **(オプション)** |

**必要なモデルファイル (GGUF 形式):**

| 用途 | 推奨モデル |
|------|-----------|
| GM (高品質) | Gemma-4B 相当以上 |
| NPC / Support (軽量) | Gemma-2B 相当 |
| 埋め込み | `nomic-embed-text-v1.5.Q4_K_M.gguf` |

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

`config.env` を編集してモデルファイルのパスを設定します:

```bash
# 必須: モデルファイルのパスを実際のパスに変更
MODEL_GM=/path/to/models/gm_model.gguf
MODEL_NPC=/path/to/models/npc_model.gguf
MODEL_EMBED=/path/to/models/nomic-embed-text-v1.5.Q4_K_M.gguf

# オプション: 画像生成を使う場合
COMFY_URL=http://<ComfyUI-HOST>:8188
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

### 4. ルールブックのインポート (推奨)

```bash
# Markdown / PDF / テキストファイルを rag_chunks テーブルに取り込む
uv run ai-agents/ocr_rulebook.py data/rulebook/swordworld2.5_rulebook1.md \
    --db data/omni.db

# 埋め込みベクターを生成する (llama-server が起動している必要あり)
uv run ai-agents/embed_chunks.py --db data/omni.db
```

### 5. フロントエンド依存関係のインストール

```bash
cd ui && npm install && cd ..
```

---

## 起動 / 停止 (Start / Stop)

### 全サービスを一括起動

```bash
./start.sh
```

起動後のアクセス先:

| サービス | URL |
|---------|-----|
| Web UI | http://localhost:5173 |
| REST API | http://localhost:8080 |
| ログ | `logs/` ディレクトリ |

### 全サービスを停止

```bash
./stop.sh
```

> `start.sh` は llama-server 6 インスタンス → Go Orchestrator → Vite Dev Server の順に起動し、ログをターミナルに tail します。

---

## 設定リファレンス (Configuration)

`config.env` で管理するすべての設定キー:

| キー | デフォルト | 説明 |
|-----|-----------|------|
| `DB_PATH` | `data/omni.db` | SQLite データベースのパス |
| `ADDR` | `:8080` | Go サーバーのリッスンアドレス |
| `COMFY_URL` | *(空)* | ComfyUI のエンドポイント (空で無効) |
| `LLAMA_SERVER` | `llama-server` | llama-server バイナリのパスまたはコマンド名 |
| `MODEL_GM` | — | GM 用モデルファイルパス **(必須)** |
| `MODEL_NPC` | — | NPC / Support 用モデルファイルパス **(必須)** |
| `MODEL_EMBED` | — | 埋め込み用モデルファイルパス **(必須)** |
| `GM_URL` | `http://localhost:11430` | GM LLM エンドポイント |
| `SUPPORT_URL` | `http://localhost:11431` | Support LLM エンドポイント |
| `NPC_A_URL` | `http://localhost:11432` | NPC-A LLM エンドポイント |
| `NPC_B_URL` | `http://localhost:11433` | NPC-B LLM エンドポイント |
| `NPC_C_URL` | `http://localhost:11434` | NPC-C LLM エンドポイント |
| `EMBED_URL` | `http://localhost:11435` | 埋め込み LLM エンドポイント |
| `PERSONAS_DIR` | `data/personas` | NPC ペルソナ YAML ファイルのディレクトリ |

---

## バッチツール (Batch Tools)

`ai-agents/` 以下の Python スクリプトはオフラインメンテナンス用です。

### ルールブックのインポート

```bash
uv run ai-agents/ocr_rulebook.py <file> [--source-type rulebook|correction|houserule] [--db PATH]
```

- `.pdf` / `.md` / `.txt` 形式に対応
- `--source-type` で優先度を制御 (`houserule`=20 → `correction`=10 → `rulebook`=0)

### 埋め込みベクターの生成

```bash
uv run ai-agents/embed_chunks.py [--db PATH] [--embed-url URL] [--batch 32]
```

- `rag_chunks` テーブルの未埋め込みチャンクを一括処理

### チャンクの最適化

```bash
uv run ai-agents/optimize_chunks.py [--db PATH]
```

- LLM でチャンクテキストを書き換え、RAG の検索精度を向上

### シナリオのバッチ生成

```bash
uv run ai-agents/generate_scenarios.py [--count N] [--rank A-E] [--model-url URL] [--comfy-url URL] [--db PATH]
```

- LLM でシナリオ JSON を生成し、ComfyUI で画像を生成して DB に保存

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
| `orchestrator/pkg/game/turn.go` | ターン処理ロジック |
| `orchestrator/pkg/llm/client.go` | llama-server クライアント |
| `orchestrator/pkg/rag/engine.go` | ベクター検索エンジン |
| `orchestrator/pkg/db/schema.sql` | SQLite スキーマ |

### React フロントエンド

```bash
cd ui

npm run dev      # Vite 開発サーバー (HMR 付き)
npm run build    # プロダクションビルド
npm run lint     # ESLint チェック
```

主要ディレクトリ:

| パス | 役割 |
|------|------|
| `ui/src/views/` | セッション・クエスト・NPC・ルール等のメインビュー |
| `ui/src/components/` | BattleMap・GMChannel・PlayerInput 等の共通コンポーネント |
| `ui/src/hooks/useGameSocket.ts` | WebSocket 管理フック |
| `ui/src/types.ts` | TypeScript 型定義 |

### Python バッチツール

```bash
cd ai-agents

# 依存関係のインストール
uv sync

# 個別スクリプトの実行
uv run <script>.py --help
```

---

## ドキュメント (Documentation)

| ファイル | 内容 |
|---------|------|
| [docs/spec_ja.md](docs/spec_ja.md) | システム仕様書 (日本語) |
| [docs/spec_en.md](docs/spec_en.md) | System Specification (English) |
| [docs/maintenance_ja.md](docs/maintenance_ja.md) | 運用手順書 (日本語) |
| [docs/maintenance_en.md](docs/maintenance_en.md) | Maintenance Guide (English) |
| [docs/tools_ja.md](docs/tools_ja.md) | バッチツールリファレンス (日本語) |
| [docs/tools_en.md](docs/tools_en.md) | Batch Tools Reference (English) |

---

## ライセンス (License)

[MIT License](LICENSE) © 2026 kazuki_fujimura
