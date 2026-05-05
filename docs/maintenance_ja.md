# SW2.5 Omni-Master — メンテナンス手順書

> バージョン: 1.0 / 言語: 日本語

---

## 1. 通常のセッション手順

### 1.1 セッション開始

```bash
# プロジェクトルートで実行
cd /Users/kf/Work/sw25-omni-master
./start.sh
```

起動順序:
1. `llama-server` x6（GM/Support/NPC×3/Embed）
2. 初期化待機 10秒
3. Go オーケストレーター（自動ビルド）
4. Vite UI dev server
5. 全ログを `tail -f` で表示

起動確認:
- UI: `http://localhost:5173`
- API: `http://localhost:8080`

### 1.2 セッション終了

```bash
./stop.sh
```

ポートを直接 LISTEN しているプロセスを特定して SIGTERM → 1秒後に SIGKILL。`.pids` ファイルも削除。

### 1.3 ログ確認

```bash
# リアルタイム（start.sh が自動で表示）
tail -f logs/orchestrator.log
tail -f logs/gm.log
tail -f logs/ui.log

# LLM 全ログ
tail -f logs/gm.log logs/support.log \
        logs/npc-a.log logs/npc-b.log logs/npc-c.log \
        logs/embed.log
```

---

## 2. データベース管理

### 2.1 セッションリセット（ルールデータ保持）

プレイ開始前やセッション間のリセット。NPC 定義・PC シートは保持。

```bash
./setup.sh
```

削除対象:
- `sessions`（カスケードで `npc_sheets`, `session_logs`, `checkpoints` も削除）
- `quests`

保持されるもの:
- `rag_chunks`（ルールブックデータ）
- `rag_chunks_opt`
- `player_characters`
- `scenarios`, `scenario_images`

実行後、セッション #1 が自動再作成されます。

### 2.2 フルリセット（全データ消去）

```bash
./setup.sh --full
```

`rag_chunks` を含む全テーブルを削除します。ルールブックの再インポートが必要になります。

### 2.3 DB 直接操作

```bash
sqlite3 data/omni.db

# よく使うクエリ
.tables                           -- テーブル一覧
SELECT count(*) FROM rag_chunks WHERE enabled=1;
SELECT * FROM scenarios ORDER BY id DESC LIMIT 5;
SELECT COALESCE(opt_status,'unprocessed'), count(*)
  FROM rag_chunks GROUP BY opt_status;
.quit
```

### 2.4 バックアップとリストア

```bash
# バックアップ（WAL モードなので cp は不安定 → sqlite3 の backup を使う）
sqlite3 data/omni.db ".backup 'data/omni_backup_$(date +%Y%m%d).db'"

# リストア
cp data/omni_backup_YYYYMMDD.db data/omni.db
```

---

## 3. ルールブック管理

### 3.1 ルールブックをインポートする

```bash
# Markdown / テキストファイルをインポート
uv run ai-agents/ocr_rulebook.py \
    data/rulebook/swordworld2.5_rulebook1.md \
    --db data/omni.db \
    --source-type rulebook

# PDF の場合（pdfplumber 使用）
uv run ai-agents/ocr_rulebook.py \
    data/rulebook/rulebook.pdf \
    --db data/omni.db
```

インポート後の確認:

```bash
sqlite3 data/omni.db \
  "SELECT count(*) FROM rag_chunks WHERE source_type='rulebook';"
```

### 3.2 埋め込みベクトルを生成する

インポート直後はベクトルが未生成（embedding=NULL）です。RAG 検索には埋め込みが必要です。

```bash
# 埋め込みサーバーが起動していることを確認（ポート 11435）
uv run ai-agents/embed_chunks.py \
    --db data/omni.db \
    --embed-url http://localhost:11435 \
    --batch 50
```

進捗確認:

```bash
uv run ai-agents/embed_chunks.py --db data/omni.db --status
```

### 3.3 チャンクを最適化する（任意・推奨）

LLM でチャンクを書き直してOCRエラー修正・ルビ除去・タグ付与・要約を行います。最適化済みチャンクは RAG 検索で優先使用されます。

```bash
# まず状況確認
uv run ai-agents/optimize_chunks.py --status

# バッチ実行（1回 20 件ずつ、システム未使用時間帯に実行推奨）
uv run ai-agents/optimize_chunks.py \
    --limit 50 \
    --model-url http://localhost:11430 \
    --embed-url http://localhost:11435

# 失敗したチャンクを再処理
uv run ai-agents/optimize_chunks.py --retry-failed --limit 20

# 特定チャンクのみ（デバッグ用）
uv run ai-agents/optimize_chunks.py --chunk-id 42 --dry-run
```

### 3.4 問題チャンクを無効化する

ルールと無関係なページ（表紙・目次・奥付）を無効化します。

```bash
# RULES 画面での操作（推奨）
# UI の RULES タブ → source_type: rulebook でフィルタ
# → 対象チャンクの「編集」→「有効/無効」トグル

# または SQL で一括操作
sqlite3 data/omni.db << 'EOF'
UPDATE rag_chunks
SET enabled = 0
WHERE source_type = 'rulebook'
  AND (
    text GLOB '*ソード・ワールド2.5*著*' OR
    text GLOB '*目次*' OR
    length(text) < 50
  );
EOF
```

### 3.5 ハウスルール・修正を追加する

RULES 画面（UI）から追加するか、直接 API を呼び出します。

```bash
# UI を使う場合: RULES タブ → 右上の「+ 追加」ボタン

# API を使う場合
curl -X POST http://localhost:8080/api/rules \
  -H 'Content-Type: application/json' \
  -d '{
    "source_type": "houserule",
    "tag": "combat",
    "text": "このキャンペーンでは精神抵抗力に+2のボーナスを適用する"
  }'
```

---

## 4. NPC ペルソナ管理

### 4.1 新しいペルソナを追加する

`data/personas/{id}.yaml` を作成します。`id` フィールドはファイル名（拡張子なし）と一致させてください。

```yaml
id: new_npc
name: キャラクター名
port: 11432         # 11432〜11434 のいずれか（既存ペルソナと被らせない）
race: 人族
classes:
  - name: ソーサラー
    level: 3
hp: 20
hp_max: 20
mp: 30
mp_max: 30
persona:
  personality: 冷静で分析的
  motivation: 古代魔法の解明
  speech: 敬語・学術的な言い回し
  priorities:
    - 遠距離から魔法攻撃を行う
  forbidden:
    - 近接戦闘に参加する
```

追加後、UI の INFORMATION タブ > 「PARTY MEMBERS」>「+ ADD」から追加できます。

### 4.2 ペルソナを更新する

YAML ファイルを直接編集した後、セッションを再起動してください（`./stop.sh && ./start.sh`）。

セッション中の緊急修正（DEV モード）:
1. UI ヘッダーの「DEV MODE」をオン
2. PARTY タブ → 対象 NPC を選択
3. ページ最下部の「DEV — Raw YAML 直接編集」セクションを展開
4. YAML を修正 → 「YAML を保存」

---

## 5. シナリオバッチ生成

### 5.1 基本的なシナリオ生成（テキストのみ）

ComfyUI が利用できない場合でも、シナリオのテキスト素材を事前生成できます。

```bash
uv run ai-agents/generate_scenarios.py \
    --count 3 \
    --rank C \
    --no-images \
    --publish-quest
```

- `--count` 本数を指定
- `--rank` ランク（A/B/C/D/E）
- `--no-images` 画像生成をスキップ
- `--publish-quest` クエストボードに自動登録

### 5.2 画像付きシナリオ生成（ComfyUI あり）

```bash
# Windows 側 ComfyUI のアドレスを指定
uv run ai-agents/generate_scenarios.py \
    --count 2 \
    --rank B \
    --comfy-url http://192.168.1.10:8188 \
    --publish-quest
```

モデルは自動検出されます。手動で指定する場合:

```bash
uv run ai-agents/generate_scenarios.py \
    --comfy-url http://192.168.1.10:8188 \
    --comfy-model "v1-5-pruned-emaonly.safetensors"
```

### 5.3 共通背景画像の事前生成

宿屋・ギルド・森など 8 種類の共通背景を一括生成します。複数シナリオで再利用されます。

```bash
uv run ai-agents/generate_scenarios.py \
    --backgrounds \
    --comfy-url http://192.168.1.10:8188
```

保存先: `data/images/backgrounds/`

### 5.4 既存シナリオの画像を再生成する

テキスト生成済みのシナリオに画像を後から追加する場合:

```bash
# シナリオ ID を確認
uv run ai-agents/generate_scenarios.py --status

# ID を指定して画像のみ再生成
uv run ai-agents/generate_scenarios.py \
    --scenario-id 3 \
    --comfy-url http://192.168.1.10:8188
```

### 5.5 生成状況確認

```bash
uv run ai-agents/generate_scenarios.py --status
```

---

## 6. プレイヤーキャラクター管理

### 6.1 PC を作成・更新する

UI の INFORMATION タブ > 「PLAYER CHARACTER」>「+ NEW」から作成します。

PCシートの JSON 形式（`json_blob` カラム）:

```json
{
  "name": "リーナ",
  "race": "エルフ",
  "adventurerLevel": 4,
  "classes": [
    {"name": "ウィザード", "level": 4, "baseSkill": 6}
  ],
  "attrs": {
    "dex": {"base": 12, "growth": 2},
    "agi": {"base": 10, "growth": 1},
    "str": {"base": 8, "growth": 0},
    "vit": {"base": 9, "growth": 1},
    "int": {"base": 16, "growth": 3},
    "spr": {"base": 14, "growth": 2}
  },
  "hpMax": 18, "hpCurrent": 18,
  "mpMax": 32, "mpCurrent": 32,
  "gold": 2500,
  "languages": "共通語、エルフ語、古代語",
  "combatFeats": "魔法の知識"
}
```

### 6.2 PC を直接 API で操作する

```bash
# 一覧
curl http://localhost:8080/api/player-characters

# 作成
curl -X POST http://localhost:8080/api/player-characters \
  -H 'Content-Type: application/json' \
  -d '{"name": "リーナ", "json_blob": "{...}"}'

# 有効化
curl -X PATCH http://localhost:8080/api/player-characters/1/activate
```

---

## 7. DEV モードの使い方

DEV モードはゲームが正常進行しない緊急時の根本対処用ツールです。**セッション進行中は慎重に使用してください。**

### 7.1 有効化

UI ヘッダー右端の「DEV MODE」ボタンをクリック。琥珀色のバナーが表示されます。

### 7.2 チェックポイント管理（INFORMATION タブ）

DEV ON 時は全チェックポイントが一覧表示され、各行に「✕」削除ボタンが現れます。

```bash
# API でも削除可能
curl -X DELETE http://localhost:8080/api/checkpoint/5
```

### 7.3 ターンログ削除（SESSION タブ）

DEV ON 時、各ターンの先頭に赤い「✕」ボタンが表示されます。クリックするとそのターンの全ログを削除します（ローカル表示と DB 両方）。

```bash
# API でも削除可能
curl -X DELETE "http://localhost:8080/api/session-log/turn/3?session_id=1"
```

### 7.4 NPC YAML 直接編集（PARTY タブ）

DEV ON 時、各 NPC の詳細ページ最下部に「DEV — Raw YAML 直接編集」セクションが表示されます。YAML を編集して保存するとDBに反映されます。構文エラーがある場合は保存が拒否されます。

---

## 8. トラブルシューティング

### UI が表示されない

```bash
# Vite が起動しているか確認
tail -20 logs/ui.log

# ポートが使用中か確認
lsof -i :5173

# 手動で UI だけ再起動
cd ui && npm run dev -- --host
```

### LLM が応答しない

```bash
# 各サーバーの起動状況確認
curl http://localhost:11430/health
curl http://localhost:11435/health

# ログ確認
tail -50 logs/gm.log
tail -50 logs/embed.log

# 個別に再起動
llama-server \
  --model /path/to/model.gguf \
  --port 11430 --host 127.0.0.1 \
  --ctx-size 4096 --n-gpu-layers 99
```

### RAG 検索が機能しない（埋め込みがない）

```bash
# 埋め込み未生成チャンクを確認
sqlite3 data/omni.db \
  "SELECT count(*) FROM rag_chunks WHERE embedding IS NULL AND enabled=1;"

# 埋め込みを生成
uv run ai-agents/embed_chunks.py \
    --embed-url http://localhost:11435
```

### ターン処理がタイムアウトする

- UI 側タイムアウト: 180秒（`SessionView.tsx` の `AbortSignal.timeout(180000)`）
- `MODEL_GM` のモデルサイズを小さいものに変更
- `--ctx-size` を `2048` に削減

### ComfyUI 画像が生成されない

```bash
# 接続確認
curl http://192.168.1.10:8188/system_stats

# config.env の COMFY_URL を確認
grep COMFY_URL config.env

# 画像生成のみスキップしてゲーム継続したい場合
# COMFY_URL= （空文字に設定）
```

### DB ロックエラー

```bash
# WAL チェックポイントを実行
sqlite3 data/omni.db "PRAGMA wal_checkpoint(FULL);"

# -shm / -wal ファイルを削除（全サービス停止後）
./stop.sh
rm -f data/omni.db-shm data/omni.db-wal
./start.sh
```

### llama-server が見つからない

```bash
# PATH に追加されているか確認
which llama-server

# config.env でフルパスを指定
echo "LLAMA_SERVER=/usr/local/bin/llama-server" >> config.env

# または LM Studio のバンドル版を使う場合
echo "LLAMA_SERVER=/Applications/LM Studio.app/Contents/Resources/llama-server" >> config.env
```

---

## 9. 定期メンテナンス推奨スケジュール

| 頻度 | 作業 | コマンド |
|------|------|---------|
| セッション前 | DB リセット | `./setup.sh` |
| セッション前 | 最新クエスト確認 | UI の QUEST BOARD |
| 月1回 | チャンク最適化バッチ | `optimize_chunks.py --limit 200` |
| 月1回 | シナリオ事前生成 | `generate_scenarios.py --count 5 --publish-quest` |
| 月1回 | DB バックアップ | `sqlite3 data/omni.db ".backup 'backup.db'"` |
| ルールブック更新時 | チャンクインポート | `ocr_rulebook.py` → `embed_chunks.py` |
| 随時 | ハウスルール追加 | RULES タブ → 「+ 追加」 |

---

## 10. サービスポート一覧

| サービス | ポート | 説明 |
|----------|--------|------|
| Vite UI | 5173 | フロントエンド開発サーバー |
| Go Orchestrator | 8080 | REST API + WebSocket |
| GM LLM | 11430 | GM役（Gemma-4B相当） |
| Support LLM | 11431 | バリデーション・リソース計算 |
| NPC-A LLM | 11432 | NPC 1体目 |
| NPC-B LLM | 11433 | NPC 2体目 |
| NPC-C LLM | 11434 | NPC 3体目 |
| Embed LLM | 11435 | ベクトル埋め込み生成 |
| ComfyUI | 8188 | 画像生成（Windows 側） |
