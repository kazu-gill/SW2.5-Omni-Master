# SW2.5 Omni-Master — システム仕様書

> バージョン: 1.0 / 言語: 日本語

---

## 1. システム概要

**SW2.5 Omni-Master** は、TRPG「ソード・ワールド2.5」のセッションをAIが自律進行するオーケストレーションシステムです。

GMとNPCをそれぞれ独立したローカルLLM（Large Language Model）が担当し、プレイヤーの行動宣言に対してリアルタイムで応答を生成します。ルールブックをベクトル検索（RAG）で参照することでルール整合性を保ち、ComfyUIによるシーン画像生成も統合しています。

### 主な特徴

| 機能 | 説明 |
|------|------|
| **GM自律進行** | LLMがGM役を担い、プレイヤーの行動に対して状況描写・判定を自動生成 |
| **NPCマルチエージェント** | 各NPCが独立したLLMインスタンスを持ち、個別の人格で行動 |
| **RAGルール参照** | インポートしたルールブックを埋め込みベクトルで検索し、LLMに注入 |
| **画像生成** | ComfyUI（Stable Diffusion）でシーン画像・NPCポートレートを生成 |
| **リアルタイムUI** | WebSocketで全クライアントにターン結果をリアルタイム配信 |
| **チェックポイント** | セッション状態を自動保存・復元（最大10件） |
| **バッチメンテナンス** | シナリオ事前生成・チャンク最適化をオフライン実行 |

---

## 2. アーキテクチャ

```
┌───────────────────────────────────────────────────────┐
│                    ブラウザ (React UI)                  │
│  INFORMATION │ SESSION │ QUEST BOARD │ PARTY │ RULES  │
└──────────────────────┬────────────────────────────────┘
                       │ HTTP / WebSocket
┌──────────────────────▼──────────────────────────────┐
│             Go オーケストレーター (:8080)              │
│  ┌──────────┐  ┌────────────┐  ┌─────────────────┐  │
│  │ REST API │  │ Game Engine │  │ WebSocket Hub  │  │
│  └──────────┘  └─────┬──────┘  └─────────────────┘  │
│                      │ HTTP                         │
└──────────────────────┼──────────────────────────────┘
                       │
        ┌──────────────┼──────────────────┐
        ▼              ▼                  ▼
  ┌───────────┐  ┌───────────┐   ┌──────────────┐
  │ GM LLM    │  │ NPC LLM   │   │ Embed LLM    │
  │ :11430    │  │ A/B/C     │   │ :11435       │
  │(Gemma-4B) │  │:11432-34  │   │(nomic-embed) │
  └───────────┘  └───────────┘   └──────────────┘
        ▲              ▲
        │ RAG Context  │
  ┌─────┴──────────────┴───────┐
  │     SQLite DB (omni.db)    │
  │  rag_chunks / sessions /   │
  │  npc_sheets / quests / ... │
  └────────────────────────────┘
        
  ┌──────────────────────────┐
  │  ComfyUI (Windows側)     │  ← オプション
  │  http://192.168.1.10:    │
  │  8188                    │
  └──────────────────────────┘

  ┌──────────────────────────┐
  │  Python バッチツール       │  ← メンテナンス専用
  │  ai-agents/*.py          │
  └──────────────────────────┘
```

---

## 3. 技術スタック

| レイヤー | 技術 | 役割 |
|----------|------|------|
| **フロントエンド** | React 18 + TypeScript + Vite | ゲームUI |
| **バックエンド** | Go 1.22+ | REST API・ゲームエンジン |
| **データベース** | SQLite (modernc/sqlite) | 全永続データ |
| **LLMランタイム** | llama-server (llama.cpp) | ローカルLLM推論 |
| **GMモデル** | Gemma-4-E4B (Q4_K_M) | GM役 |
| **NPCモデル** | Gemma-4-E2B (Q4_K_P) | NPC役・バリデーション |
| **埋め込みモデル** | nomic-embed-text-v1.5 (Q4_K_M) | RAGベクトル生成 |
| **画像生成** | ComfyUI + Stable Diffusion | シーン・ポートレート |
| **バッチツール** | Python 3.11+ + uv | ルールブック処理・シナリオ生成 |

---

## 4. ディレクトリ構成

```
sw25-omni-master/
├── orchestrator/               # Go バックエンド
│   ├── cmd/server/main.go      # エントリーポイント
│   └── pkg/
│       ├── api/handler.go      # HTTPルート・WebSocket
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
│       │   ├── validate.go     # アクション検証
│       │   └── comfyui.go      # 画像生成クライアント
│       ├── llm/                # LLMクライアント
│       └── rag/                # ベクトル検索エンジン
├── ui/                         # React フロントエンド
│   └── src/
│       ├── App.tsx             # ルート・ナビゲーション
│       ├── views/              # 画面コンポーネント
│       ├── hooks/              # useGameSocket.ts
│       └── utils/              # questEligibility.ts
├── ai-agents/                  # Python バッチツール
│   ├── ocr_rulebook.py         # ルールブックインポート
│   ├── embed_chunks.py         # 埋め込み生成
│   ├── optimize_chunks.py      # チャンク最適化
│   └── generate_scenarios.py   # シナリオバッチ生成
├── data/
│   ├── omni.db                 # SQLiteデータベース
│   ├── personas/               # NPC定義YAMLファイル
│   ├── rulebook/               # インポート元ルールブック
│   └── images/                 # 生成画像
│       ├── backgrounds/        # 共通背景画像
│       └── scenarios/{id}/     # シナリオ別画像
├── docs/                       # ドキュメント
├── logs/                       # サービスログ
├── config.env                  # 設定ファイル
├── setup.sh                    # DB初期化・リセット
├── start.sh                    # 全サービス起動
└── stop.sh                     # 全サービス停止
```

---

## 5. 設定ファイル (config.env)

`config.env` に記載した値は `start.sh` と Go サーバーが起動時に読み込みます。OS 環境変数 > config.env > デフォルト値 の優先順で適用されます。

| キー | デフォルト値 | 説明 |
|------|-------------|------|
| `DB_PATH` | `data/omni.db` | SQLite データベースファイルパス |
| `ADDR` | `:8080` | Go サーバーのリッスンアドレス |
| `COMFY_URL` | `""` (空=無効) | ComfyUI のエンドポイント URL |
| `LLAMA_SERVER` | `llama-server` | llama-server バイナリ名・パス |
| `MODEL_GM` | (必須) | GM役 LLM モデルファイルの絶対パス |
| `MODEL_NPC` | (必須) | NPC役・サポート LLM モデルの絶対パス |
| `MODEL_EMBED` | (必須) | 埋め込みモデルの絶対パス |
| `GM_URL` | `http://localhost:11430` | GM LLM エンドポイント |
| `SUPPORT_URL` | `http://localhost:11431` | バリデーション LLM エンドポイント |
| `NPC_A_URL` | `http://localhost:11432` | NPC-A LLM エンドポイント |
| `NPC_B_URL` | `http://localhost:11433` | NPC-B LLM エンドポイント |
| `NPC_C_URL` | `http://localhost:11434` | NPC-C LLM エンドポイント |
| `EMBED_URL` | `http://localhost:11435` | 埋め込み LLM エンドポイント |
| `PERSONAS_DIR` | `data/personas` | NPC ペルソナ YAML ファイルの格納ディレクトリ |

---

## 6. データベーススキーマ

### 6.1 sessions
セッション単位のメタデータ。現在はセッション ID=1 を常に使用。

| カラム | 型 | 説明 |
|--------|-----|------|
| `id` | INTEGER PK | セッションID |
| `quest_id` | TEXT | 受注中クエストID |
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
| `position_x` / `position_y` | INTEGER | バトルマップ座標（Y: 0-2=敵/3-5=前衛/6-7=後衛） |
| `yaml_blob` | TEXT | NPC定義YAML（ペルソナ含む） |
| `updated_at` | DATETIME | 更新日時 |

### 6.3 session_logs
全ターンの発言・行動記録。

| カラム | 型 | 説明 |
|--------|-----|------|
| `id` | INTEGER PK | ログID |
| `session_id` | INTEGER FK | セッションID |
| `turn` | INTEGER | ターン番号 |
| `role` | TEXT | `player` / `gm` / `npc` / `support` |
| `content` | TEXT | 発言内容 |
| `validated_at` | DATETIME | バリデーション済み時刻 |

### 6.4 checkpoints
ターン単位のスナップショット（最大10件）。

| カラム | 型 | 説明 |
|--------|-----|------|
| `id` | INTEGER PK | チェックポイントID |
| `session_id` | INTEGER FK | セッションID |
| `turn` | INTEGER | 保存ターン番号 |
| `snapshot_json` | TEXT | ゲーム状態の完全スナップショット（JSON） |
| `created_at` | DATETIME | 保存日時 |

### 6.5 rag_chunks
ルールブックの分割チャンク（RAG検索のソース）。

| カラム | 型 | 説明 |
|--------|-----|------|
| `id` | INTEGER PK | チャンクID |
| `source_type` | TEXT | `rulebook` / `correction` / `houserule` |
| `priority` | INTEGER | `20`=ハウスルール / `10`=修正 / `0`=ルールブック |
| `tag` | TEXT | `combat` / `magic` / `item` / `character` / `world` / `rule` / `status` / `general` |
| `text` | TEXT | チャンクテキスト |
| `enabled` | INTEGER | 1=有効 / 0=無効（RAG検索対象外） |
| `overrides_id` | INTEGER FK | 修正対象のチャンクID |
| `embedding` | BLOB | float32ベクトル（リトルエンディアン） |
| `opt_status` | TEXT | `NULL`=未処理 / `done` / `failed` / `skip` |

### 6.6 rag_chunks_opt
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

### 6.7 player_characters
プレイヤーキャラクターシート。

| カラム | 型 | 説明 |
|--------|-----|------|
| `id` | INTEGER PK | キャラクターID |
| `name` | TEXT | キャラクター名 |
| `json_blob` | TEXT | 能力値・スキル・HP等を含むJSONシート |
| `is_active` | INTEGER | 1=セッション中に使用中（1体のみ） |
| `created_at` / `updated_at` | DATETIME | 作成・更新日時 |

### 6.8 quests
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

### 6.9 scenarios
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

### 6.10 scenario_images
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

### ゲームエンジン

| メソッド | パス | 説明 |
|--------|------|------|
| `POST` | `/api/turn` | ターン処理（プレイヤー行動宣言） |
| `POST` | `/api/gm-channel` | GM直訴（GMによる直接介入） |
| `GET` | `/api/session/{id}` | セッション状態取得 |
| `GET` | `/api/checkpoint/restore/{turn}` | チェックポイント取得 |
| `GET` | `/api/comfy/status` | ComfyUI 接続確認 |

### NPC・パーティー管理

| メソッド | パス | 説明 |
|--------|------|------|
| `GET` | `/api/personas` | ペルソナ一覧 |
| `POST` | `/api/npc-sheet` | NPC追加 |
| `DELETE` | `/api/npc-sheet` | NPC削除 |
| `PATCH` | `/api/npc-sheet/{id}` | NPC状態更新（HP/MP/YAML） |
| `PATCH` | `/api/npc-position` | NPC隊列変更 |

### プレイヤーキャラクター

| メソッド | パス | 説明 |
|--------|------|------|
| `GET` | `/api/player-characters` | PC一覧 |
| `POST` | `/api/player-characters` | PC作成 |
| `PUT` | `/api/player-characters/{id}` | PC更新 |
| `DELETE` | `/api/player-characters/{id}` | PC削除 |
| `PATCH` | `/api/player-characters/{id}/activate` | PCをアクティブに設定 |
| `PATCH` | `/api/player-characters/{id}/deactivate` | PCを非アクティブに |

### クエスト

| メソッド | パス | 説明 |
|--------|------|------|
| `GET` | `/api/quests` | クエスト一覧 |
| `PATCH` | `/api/quests/{id}/accept` | クエスト受注 |
| `PATCH` | `/api/quests/{id}/complete` | クエスト完了 |

### ルール管理

| メソッド | パス | 説明 |
|--------|------|------|
| `GET` | `/api/rules` | チャンク一覧（`?source_type=` フィルタ可） |
| `POST` | `/api/rules` | ハウスルール・修正追加 |
| `PATCH` | `/api/rules/{id}` | チャンク更新 |
| `DELETE` | `/api/rules/{id}` | チャンク削除 |

### シナリオ

| メソッド | パス | 説明 |
|--------|------|------|
| `GET` | `/api/scenarios` | シナリオ一覧（`?status=` フィルタ可） |
| `GET` | `/api/scenarios/{id}` | シナリオ詳細 |
| `GET` | `/api/scenarios/backgrounds` | 共通背景画像一覧 |
| `GET` | `/api/scenarios/{id}/image/{imgid}` | 画像ファイル配信 |
| `PATCH` | `/api/scenarios/{id}/status` | ステータス更新 |

### DEV モード（開発者専用）

| メソッド | パス | 説明 |
|--------|------|------|
| `DELETE` | `/api/checkpoint/{id}` | チェックポイント削除 |
| `DELETE` | `/api/session-log/turn/{turn}` | ターンログ削除（`?session_id=` 必須） |

### WebSocket

| パス | 説明 |
|------|------|
| `/ws` | WebSocket 接続。`TurnResult` と `ImageUpdate` をブロードキャスト |

---

## 8. ゲームエンジン処理フロー

```
プレイヤー → POST /api/turn
                    │
           ┌────────▼─────────┐
           │  RAG 検索         │ ← rag_chunks をコサイン類似度で検索
           │  (関連ルール抽出)  │   優先度: houserule > correction > rulebook
           └────────┬────────┘
                    │ コンテキスト注入
           ┌────────▼────────┐
           │  GM LLM         │ ← :11430（Gemma-4B）
           │  状況描写生成     │   NPC行動提案を含むYAML出力
           └────────┬────────┘
                    │ 並列実行
         ┌──────────┼──────────┐
         ▼          ▼          ▼
    NPC-A LLM  NPC-B LLM  NPC-C LLM    ← :11432〜11434
    個別行動   個別行動   個別行動       （各NPCの人格に基づく）
         └──────────┼──────────┘
                    │ 全NPC行動
           ┌────────▼────────┐
           │  Support LLM    │ ← :11431
           │  ルール検証       │   HP/MP変化量を計算
           │  リソース計算     │
           └────────┬────────┘
                    │
           ┌────────▼────────┐
           │  DB 状態更新      │ ← npc_sheets HP/MP更新
           │  ログ記録         │   session_logs 追記
           │  チェックポイント  │   checkpoints 保存（自動）
           └────────┬────────┘
                    │ WebSocket ブロードキャスト
           ┌────────▼────────┐
           │  ComfyUI 画像    │ ← オプション（非同期）
           │  生成・配信       │   ImageUpdate を別途ブロードキャスト
           └─────────────────┘
```

---

## 9. フロントエンド画面構成

| 画面名 | ナビ表示 | 説明 |
|--------|----------|------|
| **INFORMATION** | `start` | セッション準備: NPC選択・PC確認・チェックポイントロード |
| **SESSION** | `session` | アクティブセッション: チャットログ・行動入力・バトルマップ・クエスト受注 |
| **QUEST BOARD** | `quest` | クエスト一覧・パーティレベルによる適性フィルタ |
| **PARTY** | `npc` | NPCステータス詳細・ポートレートアルバム・能力値編集 |
| **RULES** | `rules` | ルールブック検索・ハウスルール追加・修正エントリ管理 |

### DEV モード
ヘッダーの「DEV MODE」ボタンで有効化。以下の機能が解放されます：

- INFORMATION: チェックポイント個別削除
- SESSION: ターンログ個別削除（チャット欄の ✕ ボタン）
- PARTY: NPC の YAML 直接編集

---

## 10. NPC ペルソナ YAML 仕様

`data/personas/{id}.yaml` に配置します。

```yaml
id: gard                    # システムID（ファイル名と一致推奨）
name: ガルド                # 表示名
port: 11432                 # 使用するllama-serverポート (11432〜11434)

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

position:                   # バトルマップ初期座標
  x: 2
  y: 5                      # 0-2=敵後衛/3-5=前衛/6-7=味方後衛

equipment:
  weapon: バトルアックス+1
  armor: チェインメイル
  shield: ラージシールド

skills:
  - name: 武器習熟（斧・鎚系）
    rank: 4
    type: combat             # combat / magic / general
    note: 斧・鎚系武器の攻撃ロールに+1

consumables:
  - name: 魔法の矢
    cur: 3
    max: 5

persona:                    # LLMシステムプロンプトに注入
  personality: 無骨で短気。仲間への忠誠心は厚い。
  motivation: 仲間を守ること。
  speech: 短文・体言止め多用。「〜だ」「〜する」
  priorities:
    - 前衛で敵の攻撃を引き受ける
    - HP 10以下になったら防御を優先
  forbidden:
    - 敵が降伏の意思を示した場合に止めを刺す行為
    - 女性・子どもへの攻撃
```

### NPC LLM 出力フォーマット（固定YAML）

```yaml
name: ガルド
action: 敵に向かって斧を振り下ろす
dice: 2d6+8
target: ゴブリン戦士
dialogue: 「そこを動くな！」
hp_delta: -15       # 負=ダメージ / 正=回復
mp_cost: 2
new_lane: front     # 隊列変更時のみ（enemy/front/party）
```

---

## 11. ComfyUI 連携仕様

### 接続
- Windows マシンで ComfyUI を起動し、`COMFY_URL` に LAN アドレスを設定
- `COMFY_URL` が空の場合、画像生成は完全に無効（ゲームの進行には影響なし）

### ワークフロー
標準的な txt2img パイプラインを使用：

```
CheckpointLoaderSimple → CLIPTextEncode (positive/negative)
    → EmptyLatentImage → KSampler → VAEDecode → SaveImage
```

- ステップ数: 25
- CFG Scale: 7.0
- サンプラー: Euler Ancestral / スケジューラ: Karras
- シーン画像: 768×512 px（横長）
- ポートレート: 512×768 px（縦長）

### 画像取得フロー
1. `POST /prompt` → `prompt_id` 取得
2. `GET /history/{prompt_id}` を2秒ごとにポーリング（最大120〜180秒）
3. 完了後 `GET /view?filename=...&type=output` でダウンロード

---

## 12. RAG（ルール検索）仕様

### チャンク作成
- ルールブック（PDF/Markdown）を `ocr_rulebook.py` でチャンク化
- チャンクサイズ: 約500文字（セクション境界で分割）
- `embed_chunks.py` でベクトル化（nomic-embed-text-v1.5）

### 検索アルゴリズム
1. 入力テキストを埋め込みベクトルに変換
2. `rag_chunks` 全行とコサイン類似度を計算
3. 類似度順ソート → 同値は `priority` で優先（ハウスルール最優先）
4. `rag_chunks_opt` が存在するチャンクはそちらのテキスト・ベクトルを優先使用
5. 上位K件をGM/NPCのLLMプロンプトに注入

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
