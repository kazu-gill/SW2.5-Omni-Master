# SW2.5 Omni-Master — システム仕様書

> バージョン: 1.0 / 言語: 日本語

---

## 1. システム概要

**SW2.5 Omni-Master** は、TRPG「ソード・ワールド2.5」のセッションをAIが自律進行するオーケストレーションシステムです。

GM・NPC・サポート（リソース検証）を、ローカルで動く 1 つのチャット LLM（Large Language Model）が直列に担当し、プレイヤーの行動宣言に対して応答を生成します。LLM は別リポジトリの推論基盤 mlx-serve（`/Users/kf/AI/MLX`）が提供し、Omni-Master 自身は推論プロセスを起動しません。ルールブックをベクトル検索と用語インデックス（RAG）で参照することでルール整合性を保ち、MLX の画像工房（mlx-studio-web）によるシーン画像生成も統合しています。

UI とオーケストレーターの API の取り決め（エンドポイント・WebSocket のイベント・型）は [api.md](api.md) にあります。本書はその背後の処理を説明します。

### 主な特徴

| 機能 | 説明 |
|------|------|
| **GM自律進行** | LLMがGM役を担い、プレイヤーの行動に対して状況描写と判定の要求を生成 |
| **NPCマルチエージェント** | 各NPCが同じモデルを順番に使い、ペルソナ（性格・口調・経歴・行動原則）に沿って行動。段ごとに出力を検証し、不正なら再思考 |
| **セッションの記憶** | セッション固定の前置き（卓の約束・受注クエスト・PC・NPCペルソナ）、あらすじ、まだあらすじにしていないターン（2〜4 ターン）の原文をLLMに渡す |
| **判定** | GM・NPCは判定の種類・基準値・目標値だけを書き、2d6はコードが振る |
| **RAGルール参照** | インポートしたルールブックを埋め込みベクトルと用語の読みで検索し、LLMに注入 |
| **画像生成** | MLX の画像工房（mlx-studio-web）で数ターンごとにシーン画像を生成 |
| **リアルタイムUI** | WebSocketで段ごとの進行とGM・NPCの本文を逐次配信 |
| **セッション管理** | 複数セッションの作成・削除、中断・やり直し・任意のターンの開始前への巻き戻し（チェックポイントは最新10件） |
| **バッチメンテナンス** | ルールブックのチャンク化・埋め込み生成・シナリオ事前生成をオフライン実行 |

---

## 2. アーキテクチャ

```
┌───────────────────────────────────────────────────────┐
│                    ブラウザ (React UI)                  │
│       セッション │ クエスト │ パーティ │ ルール        │
└──────────────────────┬────────────────────────────────┘
                       │ HTTP / WebSocket
┌──────────────────────▼──────────────────────────────┐
│             Go オーケストレーター (:8080)              │
│  ┌──────────┐  ┌────────────┐  ┌─────────────────┐  │
│  │ REST API │  │ Game Engine │  │ WebSocket Hub  │  │
│  └──────────┘  └─────┬──────┘  └─────────────────┘  │
│                      │ HTTP (OpenAI 互換)            │
└──────────────────────┼──────────────────────────────┘
                       │
┌──────────────────────▼──────────────────────────────┐
│  mlx-serve (/Users/kf/AI/MLX, 別リポジトリ)           │
│  ┌─────────────────────────────┐ ┌────────────────┐ │
│  │ Chat LLM :11441              │ │ Embed :11455   │ │
│  │ gemma4-31b-it-heretic-4bit-  │ │ bge-m3-q8      │ │
│  │ mtp (GM / NPC / Support /    │ │ (1024 次元)    │ │
│  │ 要約)                        │ │                │ │
│  └─────────────────────────────┘ └────────────────┘ │
│  ┌─────────────────────────────────────────────────┐ │
│  │ mlx-studio-web :11462（画像工房、オプション）      │ │
│  │ 構図は Gemma、描画は Windows 側の ComfyUI          │ │
│  └─────────────────────────────────────────────────┘ │
└─────────────────────────────────────────────────────┘
        ▲
        │ RAG Context
  ┌─────┴──────────────────────┐
  │     SQLite DB (omni.db)    │
  │  rag_chunks / sessions /   │
  │  npc_sheets / turn_results │
  │  / session_summaries / ... │
  └────────────────────────────┘

  ┌──────────────────────────┐
  │  Python バッチツール       │  ← メンテナンス専用
  │  ai-agents/*.py          │
  └──────────────────────────┘
```

---

## 3. 技術スタック

| レイヤー | 技術 | 役割 |
|----------|------|------|
| **フロントエンド** | React 19 + TypeScript + Vite | ゲームUI |
| **バックエンド** | Go 1.26+ | REST API・ゲームエンジン |
| **データベース** | SQLite (modernc/sqlite) | 全永続データ |
| **LLMランタイム** | mlx-serve（別リポジトリ） | ローカルLLM推論（OpenAI 互換 API） |
| **チャットモデル** | Gemma 4 31B-it (`gemma4-31b-it-heretic-4bit-mtp`, mlx-vlm + MTP) | GM・NPC・サポートを 1 モデルで直列処理 |
| **埋め込みモデル** | BGE-M3 (`bge-m3-q8`, 1024 次元) | RAGベクトル生成 |
| **画像生成** | mlx-studio-web（MLX の画像工房） | シーン画像。バッチツールのシナリオ画像は ComfyUI を直接使う |
| **バッチツール** | Python 3.14+ + uv | ルールブック処理・シナリオ生成 |

---

## 4. ディレクトリ構成

```
sw25-omni-master/
├── orchestrator/               # Go バックエンド
│   ├── cmd/server/main.go      # エントリーポイント
│   └── pkg/
│       ├── api/
│       │   ├── handler.go      # HTTPルート・WebSocket
│       │   ├── sessions.go     # セッション API・ターンの実行（排他・中断・やり直し・巻き戻し）
│       │   └── extras.go       # 稼働状態・ルール検索・シーン画像
│       ├── config/config.go    # 設定ローダー
│       ├── db/                 # SQLiteアクセス層
│       │   ├── schema.sql      # テーブル定義
│       │   ├── sessions.go
│       │   ├── checkpoints.go
│       │   ├── rag.go
│       │   ├── scenarios.go
│       │   ├── quests.go
│       │   └── player_characters.go
│       ├── game/               # ゲームロジック
│       │   ├── turn.go         # ターン処理オーケストレーション
│       │   ├── types.go        # データ型定義
│       │   ├── stage.go        # 段ごとの呼び出し・検証・再思考
│       │   ├── validate.go     # GM / NPC / Support 出力の検証
│       │   ├── context.go      # セッション固定の前置き・直近ターン
│       │   ├── summary.go      # セッションのあらすじ
│       │   ├── checks.go       # 判定（2d6）
│       │   └── transcript.go   # 1 ターン = 1 本の会話
│       ├── images/studio.go    # MLX 画像工房のクライアント
│       ├── llm/                # LLMクライアント（mlx-serve 向け、SSE ストリーミング）
│       └── rag/                # ベクトル検索エンジン・用語インデックス（terms.go）
├── ui/                         # React フロントエンド
│   └── src/
│       ├── App.tsx             # ルート・ナビゲーション
│       ├── views/              # 開始・session/・クエスト・party/・rules/
│       ├── hooks/              # useSessionGame.ts・useSessionSocket.ts・useSystemStatus.ts
│       ├── live/turnReducer.ts # 進行中のターンの表示
│       ├── styles/             # theme.css・ui.css（MLX の画面と共通の見た目）
│       ├── dev/                # ?mock=1 の作り物の API
│       └── utils/              # npcYaml.ts・questEligibility.ts など
├── ai-agents/                  # Python バッチツール
│   ├── mlx_client.py           # mlx-serve 呼び出しの共通ヘルパー
│   ├── build_rulebook.py       # ルールブックの（再）構築
│   ├── rulebook_chunker.py     # ルールブック Markdown のチャンク化
│   ├── ocr_rulebook.py         # ハウスルール・訂正テキストのインポート
│   ├── embed_chunks.py         # 埋め込み生成
│   ├── optimize_chunks.py      # チャンク最適化（任意・旧方式）
│   ├── tests/                  # pytest（チャンク化のユニットテスト）
│   └── generate_scenarios.py   # シナリオバッチ生成
├── data/
│   ├── omni.db                 # SQLiteデータベース
│   ├── backup/                 # DB バックアップ（git 管理外）
│   ├── personas/               # NPC定義YAMLファイル
│   ├── rulebook/               # インポート元ルールブック
│   └── images/                 # 生成画像
│       ├── backgrounds/        # 共通背景画像
│       ├── scenarios/{id}/     # シナリオ別画像
│       └── sessions/{id}/      # セッションのシーン画像（IMAGES_DIR）
├── docs/                       # ドキュメント
├── logs/                       # サービスログ
├── config.env                  # 設定ファイル
├── scripts/
│   ├── Start-Omni.ps1          # オーケストレーター + UI 起動
│   └── Stop-Omni.ps1           # オーケストレーター + UI 停止
└── setup.sh                    # DB初期化・リセット
```

---

## 5. 設定ファイル (config.env)

`config.env` に記載した値は `scripts/Start-Omni.ps1` と Go サーバーが起動時に読み込みます。OS 環境変数 > config.env > デフォルト値 の優先順で適用されます。

| キー | デフォルト値 | 説明 |
|------|-------------|------|
| `DB_PATH` | `data/omni.db` | SQLite データベースファイルパス |
| `ADDR` | `:8080` | Go サーバーのリッスンアドレス |
| `LLM_URL` | `http://127.0.0.1:11441` | チャットモデル（mlx-serve プロファイル）のエンドポイント。GM・NPC・サポート・要約共通 |
| `LLM_MODEL_ID` | `gemma4-31b-it-heretic-4bit-mtp` | 全リクエストに付ける `model`。mlx-serve のプロファイル名と一致させる |
| `LLM_TIMEOUT_SEC` | `300` | LLM 1 リクエストのタイムアウト（秒） |
| `STAGE_RETRY_LIMIT` | `2` | 出力が検証を通らなかった段を再思考させる回数（0 で再試行なし） |
| `TURN_TIMEOUT_SEC` | `900` | 1 ターン全体（全段・再思考込み）の上限（秒）。超えたターンは開始前の状態に戻る |
| `APC_TENANT` | `sw25` | チャットリクエストに `X-APC-Tenant` ヘッダーで付ける名前。mlx-vlm のプレフィックスキャッシュを他のアプリと分ける |
| `IMAGES_URL` | `http://127.0.0.1:11462` | MLX の画像工房（mlx-studio-web）のエンドポイント |
| `IMAGE_EVERY` | `3` | 1 ターン目と、以降このターン数ごとにシーン画像を描く。`0` で画像なし |
| `IMAGES_DIR` | `data/images` | シーン画像の保存先（`sessions/<id>/` 以下） |
| `COMFY_URL` | `""` | `generate_scenarios.py` が使う ComfyUI。ゲーム本体は使わない |
| `EMBED_URL` | `http://127.0.0.1:11455` | 埋め込みモデル（mlx-serve プロファイル）のエンドポイント |
| `EMBED_MODEL_ID` | `bge-m3-q8` | 埋め込みリクエストに付ける `model` |
| `PERSONAS_DIR` | `data/personas` | NPC ペルソナ YAML ファイルの格納ディレクトリ |

> `LLM_MODEL_ID` / `EMBED_MODEL_ID` は mlx-serve のプロファイルの `model_id` と一致させてください。mlx-vlm は `model` がないと 422 を返し、別の名前を送ると常駐モデルを入れ替えて再ロードします。
>
> 空の値（環境変数・config.env とも）は未設定として扱われ、既定値が使われます。画像を止めるときは `IMAGES_URL` を空にせず `IMAGE_EVERY=0` にします。

---

## 6. データベーススキーマ

### 6.1 sessions
セッション単位のメタデータ。セッションは UI（`POST /api/sessions`）で作成し、いくつでも持てる。削除するとカスケードで NPC シート・ログ・チェックポイント・ターン結果・あらすじも消える。

| カラム | 型 | 説明 |
|--------|-----|------|
| `id` | INTEGER PK | セッションID |
| `title` | TEXT | セッション名（空なら UI が「セッション N」と表示） |
| `quest_id` | TEXT | 受注中クエストID（`quests.id` を文字列で。空なら未受注） |
| `created_at` | DATETIME | 作成日時 |
| `updated_at` | DATETIME | 更新日時 |

### 6.2 npc_sheets
セッション内に登場するNPCのリアルタイム状態。

| カラム | 型 | 説明 |
|--------|-----|------|
| `id` | INTEGER PK | シートID |
| `session_id` | INTEGER FK | セッションID |
| `name` | TEXT | NPC名（セッション内でユニーク） |
| `hp` / `mp` | INTEGER | 現在HP/MP |
| `hp_max` / `mp_max` | INTEGER | 最大HP/MP（UI の HP/MP バー用） |
| `position_x` / `position_y` | INTEGER | 配置。Y からレーンを決める（0-2=`enemy` 敵後衛 / 3-5=`front` フロントライン / 6-7=`party` 味方後衛） |
| `yaml_blob` | TEXT | NPC定義YAML（ペルソナ含む） |
| `updated_at` | DATETIME | 更新日時 |

### 6.3 session_logs
全ターンの発言・行動記録。

| カラム | 型 | 説明 |
|--------|-----|------|
| `id` | INTEGER PK | ログID |
| `session_id` | INTEGER FK | セッションID |
| `turn` | INTEGER | ターン番号 |
| `role` | TEXT | `player` / `gm` / `npc` / `support` / `check`（判定結果）/ `summary` |
| `content` | TEXT | 発言内容 |
| `validated_at` | DATETIME | バリデーション済み時刻 |

### 6.4 checkpoints
ターン開始前のスナップショット。セッションごとに最新10件だけ残る。

| カラム | 型 | 説明 |
|--------|-----|------|
| `id` | INTEGER PK | チェックポイントID |
| `session_id` | INTEGER FK | セッションID |
| `turn` | INTEGER | このターンの開始前の状態 |
| `snapshot_json` | TEXT | NPC シート（HP/MP・配置・YAML）のスナップショット（JSON） |
| `created_at` | DATETIME | 保存日時 |

「ターン N の前に戻す」（巻き戻し・やり直し・失敗したターンの取り消し）は、ターン N のチェックポイントから NPC シートを書き戻し、ターン N 以降のログ・ターン結果・あらすじ・チェックポイントを消す。

### 6.5 turn_results
確定したターンの結果（`TurnResult` の JSON）。UI の会話ログと、LLM に渡す直近ターンの元になる。

| カラム | 型 | 説明 |
|--------|-----|------|
| `session_id` | INTEGER FK | セッションID |
| `turn` | INTEGER | ターン番号（`session_id` と合わせて主キー） |
| `result_json` | TEXT | `TurnResult`（[api.md](api.md) の型） |
| `created_at` | DATETIME | 保存日時 |

### 6.6 session_summaries
セッションのあらすじ。1 行 1 文。

| カラム | 型 | 説明 |
|--------|-----|------|
| `id` | INTEGER PK | ID |
| `session_id` | INTEGER FK | セッションID |
| `upto_turn` | INTEGER | このターンまでを要約済み |
| `text` | TEXT | 主語付きの 1 文 |
| `created_at` | DATETIME | 作成日時 |

### 6.7 rag_chunks
ルールブックの分割チャンク・ハウスルール・訂正（RAG検索のソース）。

| カラム | 型 | 説明 |
|--------|-----|------|
| `id` | INTEGER PK | チャンクID |
| `source_type` | TEXT | `rulebook` / `correction` / `houserule` |
| `priority` | INTEGER | `20`=ハウスルール / `10`=修正 / `0`=ルールブック |
| `tag` | TEXT | ルールブック行は `build_rulebook.py` が付ける `intro` / `character` / `rule` / `combat` / `magic` / `item` / `world` / `gm` / `monster` / `scenario` / `general`。UI で追加した行は任意の文字列 |
| `text` | TEXT | チャンクテキスト |
| `enabled` | INTEGER | 1=有効 / 0=無効（RAG検索対象外） |
| `overrides_id` | INTEGER FK | 修正対象のチャンクID。この行が有効な間、参照先は RAG 検索から外れる |
| `embedding` | BLOB | float32ベクトル（リトルエンディアン） |
| `opt_status` | TEXT | `NULL`=未処理 / `done` / `failed` / `skip` |
| `source` | TEXT | 取り込み元（例: `rulebook1`）。UI・API で追加した行は空 |
| `section` | TEXT | 「部 > 章 > 項目 > 見出し」。埋め込み時は本文の前に付ける。空のこともある |
| `page_from` | INTEGER | 本のページ（範囲の先頭）。不明なら NULL |
| `page_to` | INTEGER | 本のページ（範囲の末尾）。不明なら NULL |

`source` / `section` / `page_from` / `page_to` は、既存 DB にはオーケストレーター起動時（`db.Open`）と `build_rulebook.py` / `embed_chunks.py` の実行時に自動で追加されます。

### 6.8 rag_chunks_opt
LLMで最適化済みのチャンク（RAG検索で優先使用）。

| カラム | 型 | 説明 |
|--------|-----|------|
| `chunk_id` | INTEGER FK UNIQUE | 元チャンクID |
| `text` | TEXT | 最適化済みテキスト |
| `tag` | TEXT | LLMが付与したタグ |
| `summary` | TEXT | 一行要約（30文字以内） |
| `embedding` | BLOB | 最適化テキストのベクトル |
| `model` | TEXT | 使用したモデル名 |
| `optimized_at` | DATETIME | 最適化日時 |

### 6.9 player_characters
プレイヤーキャラクターシート。

| カラム | 型 | 説明 |
|--------|-----|------|
| `id` | INTEGER PK | キャラクターID |
| `name` | TEXT | キャラクター名 |
| `json_blob` | TEXT | 能力値・スキル・HP等を含むJSONシート |
| `is_active` | INTEGER | 1=セッション中に使用中（1体のみ） |
| `created_at` / `updated_at` | DATETIME | 作成・更新日時 |

### 6.10 quests
クエストボードのクエスト一覧。

| カラム | 型 | 説明 |
|--------|-----|------|
| `id` | INTEGER PK | クエストID |
| `rank` | TEXT | `A` / `B` / `C` / `D` / `E` |
| `title` | TEXT | クエスト名 |
| `description` | TEXT | 概要 |
| `client` | TEXT | 依頼人 |
| `reward` | TEXT | 報酬 |
| `target` | TEXT | 討伐・達成対象 |
| `level` | TEXT | 推奨レベル（例: `冒険者レベル3〜5`） |
| `tags` | TEXT | `combat,explore,dungeon,social` のカンマ区切り |
| `status` | TEXT | `available` / `active` / `completed` |

### 6.11 scenarios
バッチ生成されたシナリオ素材。

| カラム | 型 | 説明 |
|--------|-----|------|
| `id` | INTEGER PK | シナリオID |
| `title` | TEXT | シナリオタイトル |
| `rank` | TEXT | 難易度ランク |
| `summary` | TEXT | 一行要約 |
| `description` | TEXT | GM向けシナリオ概要 |
| `locations_json` | TEXT | ロケーション配列（JSON） |
| `enemies_json` | TEXT | 敵情報配列（JSON） |
| `plot_hooks_json` | TEXT | 導入フック配列（JSON） |
| `events_json` | TEXT | イベント配列（JSON） |
| `status` | TEXT | `draft` / `ready` / `used` |

### 6.12 scenario_images
シナリオに紐づく生成画像（`scenario_id=NULL` は共通背景）。

| カラム | 型 | 説明 |
|--------|-----|------|
| `scenario_id` | INTEGER FK | シナリオID（NULLで共通背景） |
| `category` | TEXT | `scene` / `portrait` / `background` |
| `label` | TEXT | 識別ラベル |
| `file_path` | TEXT | 画像ファイルの絶対パス |
| `prompt_text` | TEXT | 生成に使用したプロンプト |
| `width` / `height` | INTEGER | 画像サイズ（px） |

---

## 7. API エンドポイント

エンドポイント・本文・返り値・WebSocket のイベント・型の正本は [api.md](api.md) です。ここでは全体の形だけを示します。

| 分類 | 主なエンドポイント |
|------|------------------|
| システム | `GET /api/system`（LLM・埋め込み・画像工房の稼働状態と `apc_tenant`） |
| セッション | `GET/POST /api/sessions`、`GET/DELETE /api/sessions/{id}` |
| ターン | `POST /api/sessions/{id}/turn`（202。実行中は 409）、`/cancel`、`/redo`、`/restore`、`/quest` |
| シーン画像 | `GET /api/sessions/{id}/images/{file}` |
| パーティ | `GET /api/personas`、`POST/DELETE /api/npc-sheet`、`PATCH /api/npc-sheet/{id}`、`PATCH /api/npc-position` |
| PC | `GET/POST /api/player-characters`、`PUT/DELETE /api/player-characters/{id}`、`PATCH .../{id}/activate`・`/deactivate` |
| クエスト | `GET /api/quests`、`PATCH /api/quests/{id}/accept`・`/complete` |
| ルール | `GET /api/rules`（ページング・絞り込み）、`GET /api/rules/search`（ゲーム中と同じ検索）、`POST /api/rules`、`PATCH/DELETE /api/rules/{id}` |
| シナリオ | `GET /api/scenarios`、`GET /api/scenarios/{id}`、`GET /api/scenarios/backgrounds`、`GET /api/scenarios/{id}/image/{imgid}`、`PATCH /api/scenarios/{id}/status` |
| その他 | `DELETE /api/checkpoint/{id}`（チェックポイント削除。UI からは使わない） |
| WebSocket | `/ws?session_id={id}`。`stage` / `delta` / `rules` / `check` / `turn` / `error` / `image` / `busy` をサーバーから送る |

ルールの追加・編集（`POST /api/rules`・`PATCH /api/rules/{id}`）は保存後すぐ埋め込みを作ります。埋め込みに失敗しても（mlx-serve の埋め込みプロファイルが停止中など）ルールは保存され、その行は `embedding` が NULL のまま残るので、後で `embed_chunks.py` で埋め込みます。

---

## 8. ゲームエンジン処理フロー

すべての LLM 段は同じチャットモデル（`LLM_URL`）を 1 段ずつ直列に呼びます。1 セッションで同時に走るターンは 1 つで、実行中（あらすじの更新中を含む）の送信・やり直し・巻き戻し・削除は 409 になります。

```
プレイヤー → POST /api/sessions/{id}/turn  → 202 {"turn": N}（ターン番号はサーバーが決める）
                    │                         以降の進行は WebSocket で配信
           ┌────────▼─────────┐
           │  チェックポイント  │ ← ターン N の開始前の NPC シートを保存（最新 10 件）
           └────────┬─────────┘
           ┌────────▼─────────┐
           │  セッション文脈    │ ← 固定の前置き + あらすじ + 未要約のターン（§8.4）
           └────────┬─────────┘
           ┌────────▼─────────┐
           │  RAG 検索         │ ← プレイヤー入力でルールを検索（上位 5 件、§12）
           └────────┬─────────┘
           ┌────────▼─────────┐
           │  GM               │ ← 状況描写を逐次生成。「判定: …」行があればコードが 2d6 を振る
           └────────┬─────────┘   （GM 直訴はここで終わり。NPC と数値は動かない）
           ┌────────▼─────────┐
           │  NPC 1 → 2 → 3   │ ← シート順に 1 体ずつ（ペルソナに基づく）。逐次生成
           │  個別行動 (YAML)  │   `check` があればコードが 2d6 を振る
           └────────┬─────────┘
           ┌────────▼─────────┐
           │  Support          │ ← NPC の行動で RAG 検索（上位 3 件）し、判定結果を踏まえて
           │                   │   HP/MP変化量を確定（JSON 配列）
           └────────┬─────────┘
           ┌────────▼─────────┐
           │  DB 状態更新      │ ← npc_sheets HP/MP・隊列、turn_results、session_logs
           └────────┬─────────┘
                    │ WebSocket: turn（確定したターン）
           ┌────────▼─────────┐
           │  あらすじ更新     │ ← 未要約のターンが 3 つたまったら（§8.4）
           └────────┬─────────┘
           ┌────────▼─────────┐
           │  シーン画像       │ ← 1 ターン目と IMAGE_EVERY ターンごと（非同期、§11）
           └──────────────────┘
```

GM・NPC の本文は mlx-serve から SSE で受け取り、`delta` イベントで UI に流します。各段の開始・再思考・終了は `stage` イベント、段に渡したルールは `rules`、判定は `check` で届きます。

ターンが失敗した（GM が検証を通らない、通信エラーなど）、中断された（`/cancel`）、`TURN_TIMEOUT_SEC`（既定 900 秒）を超えた場合は、ターン N の開始前の状態に戻し、`error` イベントを送ります。`LLM_TIMEOUT_SEC` は 1 リクエストごとの上限です。

やり直し（`/redo`）は指定したターンの開始前に戻してから、同じモード（行動 / 直訴）で入力を再送します（入力の差し替え可。UI は最後のターンだけをやり直す）。巻き戻し（`/restore`）は指定したターンの開始前に戻すだけです。どちらもチェックポイントが残っているターン（最新 10 件）に限られます。

### 8.1 段ごとの検証と再思考

各 LLM 段の出力は検証され、NG の場合はその段だけを呼び直します（再思考）。再思考では、却下された出力とエラー内容を会話に追加し、thinking を有効化（`enable_thinking=true`、`max_tokens` +2048）して呼び直します。再試行は最大 `STAGE_RETRY_LIMIT` 回（既定 2、つまり最大 3 回呼び出し）です。通信エラーは再試行しません。

| 段 | NG 条件 | 上限を超えたとき |
|----|--------|-----------------|
| GM | 本文（判定行を除いたもの）が空 / 途中で切れた（`finish_reason=length`）/ 拒否文 / YAML・JSON を出力している | ターンを失敗にする（開始前に戻る） |
| NPC | YAML としてパースできない / `name` がシート名と違う / `action`・`dialogue` が空 / `mp_cost` が 0〜現在MP の範囲外 / `hp_delta` < -現在HP / `new_lane` が `enemy`・`front`・`party` 以外 | その NPC だけ今回のターンを見送る（`warnings` に記録） |
| Support | JSON 配列でない / 行動した NPC ごとにちょうど 1 件でない（不明・重複の名前を含む）/ 値が範囲外 | NPC 自身の `hp_delta` / `mp_cost` を使う（`warnings` に記録） |

どの段も途中で切れた出力（`finish_reason=length`）は NG として扱います。各試行は `session_logs` に記録されます。再思考が始まると `stage` イベント（`status: retry`）が届き、UI はその段の本文を捨てて受け直します。

### 8.2 LLM リクエスト

すべてのチャットリクエスト（`/v1/chat/completions`）に次を付けます。

| 項目 | 内容 |
|------|------|
| `model` | `LLM_MODEL_ID`。mlx-vlm は省略すると 422、別の名前だと常駐モデルを再ロードする |
| `X-APC-Tenant` ヘッダー | `APC_TENANT`（既定 `sw25`）。プレフィックスキャッシュを他のアプリと分ける |
| `stream` | GM・NPC の段は `true`（SSE で受けて UI に逐次配信）。Support・要約は通常の応答 |
| `seed` | 毎回ランダム。指定しないと mlx-vlm は同じ入力に同じ文を返す |
| thinking | 通常は `{"enable_thinking": false, "chat_template_kwargs": {"enable_thinking": false}}`。再思考時のみ有効化 |

埋め込みは `/v1/embeddings` に `{"model": EMBED_MODEL_ID, "input": ...}` を送ります。

#### 1 ターン = 1 本の会話 (プレフィックスキャッシュ向け)

1 ターンの全段は、セッション文脈（§8.4）の後ろに伸びていく 1 本の会話として送ります。各段のリクエストは「前の段のリクエスト + その応答 + 自分の指示」になるため、mlx-vlm の自動プレフィックスキャッシュ (APC) が前の段までの KV を再利用し、新しい指示の分だけ prefill します。

```
system    固定の前置き (卓の約束 + 受注クエスト + PC + NPC ペルソナ) + あらすじ
user      【ターンN-2・プレイヤーの行動】…   assistant  そのターンの要点 (GM・NPC の台詞と行動・判定結果)
user      【ターンN-1・プレイヤーの行動】…   assistant  同上
user      【参照ルール】【パーティの状態】【プレイヤーの行動】【指示: GM】
assistant GM の描写 (判定行を含む)
user      【判定結果】【指示: NPC「A」】 … assistant  A の YAML
user      【判定結果】【指示: NPC「B」】 … assistant  B の YAML   ← B は A の行動を見て重ならない行動を選ぶ
user      【判定結果】【指示: NPC「C」】 … assistant  C の YAML
user      【判定結果】【指示: 数値計算担当】(Support 用のルール + 行動リスト JSON)
```

HP/MP・レーン・参照ルール・段の指示など毎ターン変わるものは、最後の user メッセージに置きます。

- 会話に残すのは検証を通った応答だけ。再思考のやり取りや見送った NPC の出力は残さない
- 再思考時は thinking を有効にするためテンプレートの先頭が変わり、キャッシュは効かない (再思考はまれなので許容)
- mlx-serve 側で APC が無効だと、各段が会話全体を毎回 prefill するため遅くなる。起動時に `/health` の `apc.enabled` を確認し、無効なら警告をログに出す。必要な mlx-serve の設定は `docs/mlx-serve-client-notes.md` を参照

### 8.3 TurnResult の主なフィールド

| フィールド | 型 | 説明 |
|-----------|----|------|
| `mode` | string | `action`（行動）/ `gm`（GM 直訴） |
| `checks` | []Check | コードが振った判定（誰・判定名・基準値・目標値・出目・合計・結果） |
| `rules_used` | []RuleHit | 段に渡したルール（用語で選ばれたものは `matched_term` 付き） |
| `stage_attempts` | map[string]int | 段ごとの LLM 呼び出し回数。キーは `gm` / `npc:<名前>` / `support`。2 以上は再思考したことを示す |
| `warnings` | []string | 検証を通らず見送り・フォールバックした段の説明 |
| `image_url` | string | シーン画像（描いたターンのみ。後から付く） |

全フィールドは [api.md](api.md) の `TurnResult` を参照してください。

### 8.4 セッションの記憶

毎ターン新しい会話を始めるのではなく、次の 3 つでセッションの流れを LLM に渡します（`pkg/game/context.go`・`summary.go`）。

| 部分 | 内容 | 置き場所 |
|------|------|---------|
| 固定の前置き | 卓の約束（役の交代・サイコロはシステムが振る など）、受注中のクエスト、使用中の PC シートの要点（種族・技能・能力値・最大HP/MP・装備・戦闘特技）、NPC のペルソナ | system メッセージ |
| あらすじ | `session_summaries` の文を古い順に | system メッセージの末尾 |
| 直近のターン | あらすじにまだ入っていないターンを原文で。最新 2 ターンは必ず入り、要約の直前には最大 4 ターンになる（プレイヤーの入力と、GM の描写・NPC の台詞と行動・判定結果） | system の後ろの user / assistant |

- 固定の前置きは変わりにくい順（卓の約束 → クエスト → PC → NPC）に並べ、ターンをまたいで同じ文になるようにしている。mlx-vlm の APC が前置きを再利用できる
- NPC ペルソナはシートの YAML から作る: 種族・性別・年齢・技能・最大HP/MP、`personality` の `tone`（性格）/ `speech_style`（口調）/ `values`（価値観）/ `fears`（恐れ）/ `quirks`（癖）、`backstory`、`action_rules`、`equipment`、`skills` の名前と説明（説明が無いと、効果を聞かれたときにモデルが作ってしまう）。無いキーは飛ばす
- あらすじ: ターンが確定するたびに、直近 2 ターンより前で未要約のターンが 3 つ以上たまっていれば、記録係の段（`stage: summary`）がそれを主語付きの 1 文ずつ（3〜6 文）にまとめて `session_summaries` に足す。要約中はセッションが実行中のまま（次のターンは 409）。失敗しても次のターンの後に再試行する

### 8.5 判定

判定の成否は LLM ではなくコードが決めます（`pkg/game/checks.go`）。

- GM は判定が要る行動に対し、描写の最後に `判定: 誰/判定名/基準値N/目標値M` の行を書く（複数なら 1 行ずつ）。この行は描写から取り除かれ、判定として振られる。GM 直訴では振らない
- NPC は YAML の任意フィールド `check: 判定名 基準値N 目標値M` で判定を求める
- 「基準値」「目標値」の語は省いてもよい（`魔物知識 10 12`、`命中力判定/10/10` も読む）。Gemma はよく省くため。数値の無い判定行（`自動成功` など）は描写から取り除くだけで振らない
- NPC の `check` が読めないときは、行動はそのまま採用し、判定だけを振らずに `warnings` に記録する（作り直しはしない）
- コードが 2d6 を振り、出目合計 + 基準値 ≥ 目標値で成功。出目 12 は自動成功、出目 2 は自動失敗
- 結果は `【判定結果（システムが振ったサイコロ）】` として後の段（NPC・Support）に渡り、`check` イベントで UI のダイスのチップになる。次のターンの GM は直近ターンの判定結果に沿って描写する

---

## 9. フロントエンド画面構成

UI は MLX の画面と共通のテーマ（`ui/src/styles/theme.css`・`ui.css`）で、ダークモードと 375px 幅に対応します。ヘッダーには LLM・埋め込み・画像工房の稼働状態（`GET /api/system`）を表示し、止まっていれば起動を促すバナーを出します。

| 画面名 | ナビ表示 | 説明 |
|--------|----------|------|
| **開始** | セッション（未選択時） | 「続きから」（最近のセッション。削除も可）と「新しく」（タイトル・クエストを選んで作成） |
| **セッション** | セッション | 会話ログ（プレイヤー・GM・NPC の吹き出し）、ターン中の段ごとの進行と逐次表示、ダイス・参照ルール・再思考・欠落のチップ、⏹ 止める・↻ 作り直す・✎ 入力を直してやり直す・⟲ このターンの前に戻す、行動 / GM直訴の入力。横のパネル（スマホでは引き出し）にパーティの HP/MP バー、3 レーンの配置、PC、クエスト、あらすじ、チェックポイント |
| **クエスト** | クエスト | クエスト一覧・パーティレベルによる適性表示、受注（開いているセッションのクエストにする）・完了 |
| **パーティ** | パーティ | このセッションの NPC（ペルソナから 3 体まで加える・外す・詳細と編集）、PC の作成・編集・使用中の切り替え。NPC の編集は YAML の未知のキーを保ったまま値だけ書き換える |
| **ルール** | ルール | ゲーム中と同じ検索、一覧のページング・絞り込み、ハウスルール・訂正の追加、有効/無効の切り替え。各チャンクの部 > 章 > 見出しとページを表示 |

### DEV モード
ヘッダーの「DEV」ボタンで有効化します。パーティ画面の NPC 詳細で YAML を直接編集できるようになります（キーを消すとペルソナが失われるので注意）。

### 開発用
- `OMNI_API=host:port npx vite` で別のオーケストレーターに向ける（既定 `localhost:8080`）
- `?mock=1` を付けて開くと、オーケストレーターなしで作り物のターンを再生する

---

## 10. NPC ペルソナ YAML 仕様

`data/personas/{id}.yaml` に配置します。

```yaml
id: gard                    # システムID（ファイル名と一致推奨）
name: ガルド                # 表示名

race: ドワーフ
gender: 男
age: 87

classes:
  - name: ファイター
    level: 4
  - name: グラップラー
    level: 2

stats:                      # 能力値（attrs/stats どちらでも可）
  str: 14
  dex: 9
  pow: 10
  int: 8
  agl: 8
  luc: 11

hp: 38
hp_max: 38
mp: 10
mp_max: 10

position:                   # 初期配置
  x: 2
  y: 5                      # 0-2=敵後衛(enemy)/3-5=フロントライン(front)/6-7=味方後衛(party)

equipment:                  # weapon / armor / shield / accessory を前置きに入れる
  weapon: バトルアックス+1
  armor: チェインメイル
  shield: ラージシールド

skills:                     # name を前置きに入れる
  - name: 武器習熟（斧・鎚系）
    description: 斧・鎚系武器の攻撃ロールに+1

personality:                # 前置き（system メッセージ）に入れる
  tone: 無骨で短気。仲間への忠誠心は厚い。
  speech_style: 短文・体言止め多用。「〜だ」「〜する」
  values: 力と誠実さを重んじる。
  fears: 仲間を守れなかったときの後悔。
  quirks: 戦闘前に斧の柄を三回叩く。

backstory: |
  元傭兵。所属した部隊が壊滅し、流浪の旅を続けている。

action_rules:
  - 敵が降伏の意思を示した場合、止めを刺さない
  - HP 10以下になったら防御を優先する
```

LLM に渡すのは §8.4 に挙げたキーだけです。ほかのキー（`stats`・`fortune` など）は LLM には渡りませんが、パーティ画面の編集でも消えません。書式の全体は `data/personas/example_gard.yaml` を参照してください。

### NPC LLM 出力フォーマット（固定YAML）

```yaml
name: ガルド
action: 敵に向かって斧を振り下ろす
dialogue: 「そこを動くな！」
mp_cost: 0          # 0〜現在MP
hp_delta: 0         # 自分の HP の変化。-現在HP〜0
new_lane: front     # 隊列変更時のみ（enemy/front/party）
check: 命中力判定 基準値5 目標値12   # 成否がサイコロで決まるときだけ（コードが振る）
```

---

## 11. シーン画像（MLX 画像工房）

### 接続
- MLX の画像工房 mlx-studio-web（`IMAGES_URL`、既定 `http://127.0.0.1:11462`）を使う。構図（タグ・画風・視点）は画像工房の Gemma が決め、描画は画像工房の先の ComfyUI が行うため、Omni-Master は ComfyUI のワークフローを持たない
- 画像工房と Gemma（mlx-serve）の両方が起動している必要がある。止まっていても、画像が付かないだけでゲームは進む
- `IMAGE_EVERY=0` で無効（空の `IMAGES_URL` は既定値に戻るため、無効化には使えない）

### タイミング
- 行動ターン（GM 直訴は除く）の 1 ターン目と、ターン番号が `IMAGE_EVERY`（既定 3）の倍数のとき。描くかどうかはコードが決める
- ターン確定後に非同期で描く（`stage: image` の進行イベント）。できたら `image` イベントを送り、ターン結果の `image_url` に付ける。待つ間にそのターンが巻き戻されていれば付けない

### 画像取得フロー
1. `POST /api/studio/infer` に依頼文（あらすじの最後の 4 文・直前の場面・プレイヤーの行動・この場面の GM の描写。1 ターンの描写だけだと場所が抜けるため）を送り、ジョブの完了をポーリングして構図を得る
2. `POST /api/studio/render` で描画し、ジョブの完了をポーリングして画像を受け取る（全体の上限 6 分）
3. `IMAGES_DIR/sessions/<セッションID>/<ターン>.<拡張子>` に保存し、`GET /api/sessions/{id}/images/{file}` で配信する

---

## 12. RAG（ルール検索）仕様

### チャンク作成
- ルールブック（OCR 済み Markdown）を `build_rulebook.py`（`rulebook_chunker.py`）でチャンク化
  - 見出し・段落単位で、文の途中では切らない（目安 600 文字。900 文字を超える段落は「。」で分割、表は 900 文字ごとに行単位で分割して見出し行を繰り返す）
  - ルビ（「漢字（かな）」と、かなの語に同じ読みを重ねた「かな（かな）」）・全角英数字・ページ番号・目次・レイアウト要素・空欄の記入用表・サンプルキャラクターの記入済みシートなどを決定的に除去
  - 目次から本のページ → 部・章・項目を引き、各チャンクに `section`（部 > 章 > 項目 > 見出し）・`page_from` / `page_to`・`tag` を付ける
  - サンプルシナリオ「蛮族を駆逐せよ」の本文は `tag='scenario'`・`enabled=0` で取り込む（GM がシナリオの筋書きを通常のセッションに持ち込まないため）
  - `--apply` は DB を `data/backup/` にバックアップしてからルールブック行（`source` が `rulebook1` または空）を置き換える。ハウスルール・訂正は残す
- 短いハウスルール集・訂正集のテキストは `ocr_rulebook.py` で追加できる（500 文字単位。ルールブック本体には使わない）
- `embed_chunks.py` でベクトル化（mlx-serve の `bge-m3-q8`、1024 次元）
  - `rag_chunks` は `section` が空でなければ「section + 改行 + 本文」を埋め込む（Go 側 `rag.EmbedInput` と同じ）。本文に話題名が出てこないチャンクも見出しで引けるようにするため
  - 埋め込みサーバが入力過大で拒否した場合（bge-m3-q8 は 1 入力 2048 トークンまで。通常のチャンクは収まる）は、本文を短くして再試行する
- UI / API（`POST /api/rules`・`PATCH /api/rules/{id}`）で追加・編集したルールは、オーケストレーターがその場で埋め込む
- 埋め込みモデルを変えた場合はベクトルの次元が変わるため、全チャンクを再埋め込みする（`embed_chunks.py --reembed-all`）
- `optimize_chunks.py`（LLM による書き直しで `rag_chunks_opt` を作る）は任意・旧方式。ルールブックには通常不要

### 検索アルゴリズム
1. 入力テキストを埋め込みベクトルに変換
2. 検索対象は `enabled=1` かつ埋め込み済みの行。ただし、有効な訂正・ハウスルールが `overrides_id` で参照しているチャンクは除外する（訂正側だけが使われる。無効な訂正は元チャンクを隠さない）
3. 類似度に優先度のボーナス（`priority` × 0.0025。ハウスルール +0.05、訂正 +0.025）を足して並べる。わずかに類似度の高いルールブック原文より、ハウスルール・訂正が上に来る
4. `rag_chunks_opt` が存在するチャンクはそちらのテキスト・ベクトルを優先使用
5. 用語インデックス（下記）で入力が用語に当たれば、その用語を定義するチャンク（最大 2 件）を先頭に置き、残りを上の順で埋める
6. 上位K件を整形してプロンプトに注入

### 用語インデックス
特技・魔法・アイテム・魔物などの名前は、埋め込みの類似度ではなく読みで確定的に引きます（`pkg/rag/terms.go`）。

- 定義チャンク: `section` の最後の見出しが《》【】〈〉で囲まれた用語で終わるチャンク。用語からルビ・ランクの数字（Ⅰ・Ⅱ…）・中黒などを除いて索引にする
- 照合は読み（カタカナ）で行う。kagome（IPA 辞書）で単語を基本形に戻してから読みにするため、「庇う」「かばう」「庇います」はどれも《かばうⅠ》に当たる
- 読みが 3 文字未満の用語は索引に入れない（短い読みがどこにでも当たるのを防ぐ）
- 索引は最初の検索時に有効なルールから作り、UI / API でルールを追加・編集・削除すると作り直す（`build_rulebook.py --apply` など DB を直接書き換えたときは、オーケストレーターを再起動すると作り直される）
- 用語で選ばれたチャンクは `matched_term` 付きで返り、UI のルールのチップに出る

`GET /api/rules/search?q=&k=` はゲーム中と同じ検索を返すので、RULES 画面から引き当てを確かめられます。

### 使用する段
| 段 | 検索クエリ | 件数 | 用途 |
|----|-----------|------|------|
| GM | プレイヤー入力 | 5 | ナラティブ・ルーリングの参照 |
| Support | NPC の行動（`action` と `check`） | 3 | 魔法の消費MPなど、参照ルールに値があればそれに従って HP/MP を確定 |
| NPC | — | — | ルールは渡さない |

検索に失敗した場合や該当がない場合は、ルールなしで処理を続けます。段に渡したルールは `rules` イベントとターン結果の `rules_used` で UI に届きます。

### コンテキストの形式

先頭に `【参照ルール】` を 1 回だけ置き、1 チャンク 1 行で並べます。

```
【参照ルール】
[ハウスルール] <本文>
[訂正] <本文>
[<section> p.<page_from>-<page_to>] <本文>
```

ルールブックのチャンクは `section` と本のページをラベルにします（1 ページだけなら `p.<page_from>`、ページ不明なら省略、`section` が空なら `source_type`）。

### 優先度
| `priority` | `source_type` | 説明 |
|------------|--------------|------|
| `20` | `houserule` | ハウスルール（最優先） |
| `10` | `correction` | OCR修正・補正 |
| `0` | `rulebook` | ルールブック原文 |

---

## 13. シナリオバッチ生成仕様

`generate_scenarios.py` が以下を生成・保存します：

```
シナリオJSON
  ├── title, rank, summary, description
  ├── client, reward, target, level
  ├── locations[]  → ComfyUI 背景画像 (768×512)
  ├── enemies[]    → ComfyUI ポートレート (512×768)
  ├── plot_hooks[] → 導入フック
  └── events[]     → イベント詳細
```

保存先：
- シナリオ本体 → `scenarios` テーブル
- 画像ファイル → `data/images/scenarios/{id}/`
- 画像パス → `scenario_images` テーブル
- 共通背景 → `data/images/backgrounds/`（`scenario_id=NULL`）

`--publish-quest` オプションで `quests` テーブルにも登録され、クエストボードに表示されます。
