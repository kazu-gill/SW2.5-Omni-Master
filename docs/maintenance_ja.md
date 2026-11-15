# SW2.5 Omni-Master — メンテナンス手順書

> バージョン: 1.0 / 言語: 日本語

---

## 1. 通常のセッション手順

### 1.1 セッション開始

LLM は mlx-serve（`~/AI/MLX`）が提供します。Omni-Master の起動スクリプトは mlx-serve を起動しないため、先に次の 2 プロファイルを起動しておきます。MLX ダッシュボード（http://127.0.0.1:11470/）から起動するか、スクリプトを使います。チャット用の LLM は同時に 1 本しか常駐できないので、ほかのチャットモデルが動いていれば Gemma に切り替えます。

```bash
pwsh ~/AI/MLX/scripts/Start-MlxServe.ps1 -Profile gemma4-31b-it-heretic-4bit-mtp   # :11441 GM/NPC/Support/要約
pwsh ~/AI/MLX/scripts/Start-MlxServe.ps1 -Profile bge-m3-q8                        # :11455 埋め込み
pwsh ~/AI/MLX/scripts/Get-MlxServeStatus.ps1                                       # 稼働確認
```

- Gemma プロファイルの起動環境には `APC_CHECKPOINT_GUARD_TOKENS=8` と `APC_CHECKPOINT_ENTRIES=4` を設定することを推奨します（プレフィックスキャッシュが段の切り替えで効くようにするため。詳細は [mlx-serve-client-notes.md](mlx-serve-client-notes.md)）
- シーン画像を使う場合は、MLX の画像工房 mlx-studio-web（:11462）も起動しておきます。構図を決めるのに Gemma を使うため、Gemma も起動している必要があります。使わない場合は `config.env` で `IMAGE_EVERY=0` にします

続いて Omni-Master を起動します（PowerShell 7）。

```bash
# プロジェクトルートで実行
cd /path/to/sw25-omni-master
pwsh scripts/Start-Omni.ps1           # ログを追従する場合は -Follow
```

起動順序:
1. `config.env` の `LLM_URL` / `EMBED_URL` の `/health` を確認（応答がなければ起動すべき `Start-MlxServe.ps1` コマンドを表示して終了）
2. Go オーケストレーター（ビルド後に起動）
3. Vite UI dev server
4. `-Follow` 指定時は `logs/orchestrator.log` と `logs/ui.log` を `tail -f` で表示

PID は `.pids` に記録されます。`.pids` が残っている場合は二重起動を防ぐため起動しません（先に `Stop-Omni.ps1` を実行）。

起動確認:
- UI: `http://localhost:5173`（ヘッダーに LLM・埋め込み・画像の稼働状態が出ます。止まっているものがあればバナーで知らせます）
- API: `http://localhost:8080`（`curl http://localhost:8080/api/system` で同じ稼働状態を確認できます）

UI を開いたら、開始画面の「新しく」でセッションを作るか、「続きから」で前のセッションを開きます。「⏸ 止まっています」と付いたセッションには止まったターンがあります（[1.4](#14-止まったターンを続ける取り消す)）。

### 1.2 セッション終了

```bash
pwsh scripts/Stop-Omni.ps1
```

UI（:5173）とオーケストレーター（`ADDR` のポート）を LISTEN しているプロセスを特定して SIGTERM → 1秒後に SIGKILL。`.pids` に残った PID も停止し、`.pids` ファイルを削除します。mlx-serve のプロファイルは停止しません。mlx-serve も止める場合:

```bash
pwsh ~/AI/MLX/scripts/Stop-MlxServe.ps1 -All
```

### 1.3 ログ確認

```bash
# リアルタイム（Start-Omni.ps1 -Follow で自動表示）
tail -f logs/orchestrator.log logs/ui.log
```

LLM の推論ログは mlx-serve 側にあります（mlx-serve の README を参照）。各段の出力と再思考の履歴（`[NG 1/3] ...`）は `session_logs` テーブルに記録されます。

### 1.4 止まったターンを続ける・取り消す

ターンが失敗（LLM に接続できない、GM の出力が検証を通らないなど）・中断（⏹ 止める）・時間切れ（`TURN_TIMEOUT_SEC`）で止まると、終わった段（GM の語り・行動した NPC・振った判定）までが DB に残り、セッション画面に「ターン N が途中で止まりました」と原因が表示されます。

| ボタン | 動作 |
|--------|------|
| ▶ 続きから | 止まった段からやり直して最後まで進める。終わった段は作り直さず、振ったサイコロもそのまま |
| 取り消す | 途中結果を捨て、そのターンを始める前の状態に戻す |
| ✎ 直す | 取り消してから、直した入力を新しいターンとして送る |

- 原因が LLM 側（mlx-serve の停止・混雑）のときは、先に mlx-serve を直してから「続きから」を押します（[8. トラブルシューティング](#8-トラブルシューティング)）
- オーケストレーターを停止・再起動しても止まったターンは残ります。ターンの実行中に停止した場合も、次の起動時に止まったターンになります（`logs/orchestrator.log` に `turn(s) left running were marked stopped` と出ます）
- 止まったターンがある間は、そのセッションで新しいターンの送信・やり直し・巻き戻し・クエストの受注・NPC の編集や入れ替え・配置の変更ができません。PC の編集・削除・使用中の切り替えは、どのセッションに止まったターンがあってもできません（メッセージにセッション名が出ます）。先に「続きから」か「取り消す」を選びます
- 止まった後に NPC の顔ぶれが変わっていると続きからは再開できず、取り消すしかありません（通常は上の制限で起きません）

DB で確かめるとき:

```bash
sqlite3 data/omni.db \
  "SELECT session_id, turn, status, error, updated_at FROM turn_progress;"
```

API で操作するとき（セッション ID が 3 の例）:

```bash
curl -X POST http://localhost:8080/api/sessions/3/resume    # 続きから
curl -X POST http://localhost:8080/api/sessions/3/discard   # 取り消す
```

---

## 2. データベース管理

### 2.1 セッションリセット（ルールデータ保持）

すべてのセッション（ターン・チェックポイント・あらすじを含む）とクエストを消すリセット。NPC 定義・PC シートは保持。

```bash
./setup.sh
```

削除対象:
- `sessions`（カスケードで `npc_sheets`, `session_logs`, `checkpoints`, `turn_results`, `session_summaries`, `turn_progress` も削除）
- `quests`

保持されるもの:
- `rag_chunks`（ルールブックデータ）
- `rag_chunks_opt`
- `player_characters`
- `scenarios`, `scenario_images`

1 つのセッションだけを消すときは、`setup.sh` を使わずに開始画面の「続きから」の 🗑 で削除します。

### 2.2 フルリセット（ルールデータも消去）

```bash
./setup.sh --full
```

2.1 の削除対象に加えて、`rag_chunks`（ルールブック・ハウスルール・訂正。カスケードで `rag_chunks_opt` も）を削除します。PC シートとシナリオは残ります。ルールブックの再インポートが必要になります。

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

バックアップは `data/backup/` に置きます（git 管理外）。`build_rulebook.py --apply` も実行前に `data/backup/omni.bak-<日時>.db` を自動で作ります。

```bash
# バックアップ（WAL モードなので cp は不安定 → sqlite3 の backup を使う）
mkdir -p data/backup
sqlite3 data/omni.db ".backup 'data/backup/omni_backup_$(date +%Y%m%d).db'"
```

リストアはサービスを止めてから行います（オーケストレーターが DB を開いたままだと上書きが反映されません）。

```bash
pwsh scripts/Stop-Omni.ps1
cp data/backup/<ファイル名>.db data/omni.db
rm -f data/omni.db-shm data/omni.db-wal
pwsh scripts/Start-Omni.ps1
```

---

## 3. ルールブック管理

### 3.1 ルールブックを（再）インポートする

ルールブック本体は `build_rulebook.py` で OCR 済み Markdown（既定 `data/rulebook/swordworld2.5_rulebook1.md`）から作り直します。見出し・段落単位で文を切らずにチャンク化し、ルビ・ページ番号・目次・レイアウト要素・空欄の記入用シートなどのノイズを取り除き、各チャンクに「部 > 章 > 見出し」と本のページを付けます。詳細は [tools_ja.md](tools_ja.md#4-build_rulebookpy) を参照してください。

手順は「確認 → 反映 → 埋め込み」の 3 段です。

```bash
# 1. 確認のみ（DB は変更しない）。タグ別のチャンク数を表示し、logs/rulebook-preview.jsonl に書き出す
uv run ai-agents/build_rulebook.py

# 2. DB のルールブック行を置き換える（実行前に data/backup/ へ自動バックアップ）
uv run ai-agents/build_rulebook.py --apply

# 3. 埋め込みを生成する（mlx-serve の bge-m3-q8 が必要）
uv run ai-agents/embed_chunks.py
```

`--apply` の動作:
- `data/backup/omni.bak-<日時>.db` にバックアップを取る
- `source_type='rulebook'` で `source` が `rulebook1`（`--source` の値）または空の行を削除し、新しいチャンクを追加する（手動で無効化していた行も含めて入れ替わる）
- ハウスルール・訂正（`houserule` / `correction`）の行は残す
- 削除対象の行を `overrides_id` で参照している訂正・ハウスルールがあると、何も変更せずに中止する。UI で参照先を付け替えてから再実行する

> `ocr_rulebook.py` は 500 文字で機械的に切るため、ルールブック本体には使いません。短いハウスルール集・訂正集のテキストを追加するときだけ使います。

`--apply` の後はオーケストレーターを再起動してください（`pwsh scripts/Stop-Omni.ps1 && pwsh scripts/Start-Omni.ps1`）。用語インデックス（特技・魔法などを読みで引く索引）は起動後の最初の検索で作られ、UI / API 以外での DB の書き換えでは作り直されません。

インポート後の確認:

```bash
sqlite3 data/omni.db \
  "SELECT tag, enabled, count(*) FROM rag_chunks WHERE source_type='rulebook' GROUP BY tag, enabled;"
```

うまく戻らない場合は、[2.4](#24-バックアップとリストア) の手順で `data/backup/` のファイルからリストアします。

### 3.2 埋め込みベクトルを生成する

インポート直後はベクトルが未生成（embedding=NULL）です。RAG 検索には埋め込みが必要です。

```bash
# mlx-serve の bge-m3-q8 が起動していることを確認（ポート 11455）
uv run ai-agents/embed_chunks.py \
    --db data/omni.db \
    --embed-url http://127.0.0.1:11455 \
    --batch 50
```

`rag_chunks` と `rag_chunks_opt` の未埋め込み行をすべて処理します。`rag_chunks` は「section（部 > 章 > 見出し）+ 改行 + 本文」を埋め込みます。表などで埋め込みサーバの入力上限（bge-m3-q8 は 1 入力 2048 トークン）を超えた行は、本文を短くして再試行します。

進捗確認:

```bash
sqlite3 data/omni.db \
  "SELECT count(*) FROM rag_chunks WHERE embedding IS NULL;"
```

埋め込みモデルを変更した場合はベクトルの次元が変わる（例: 768 → 1024）ため、既存の埋め込みを消して全件を作り直します。

```bash
uv run ai-agents/embed_chunks.py --db data/omni.db --reembed-all
```

### 3.3 チャンクを最適化する（任意・旧方式）

`optimize_chunks.py` は LLM でチャンクを書き直して `rag_chunks_opt` に保存します（OCR エラー修正・ルビ除去・タグ付与・要約）。最適化済みチャンクがあれば RAG 検索で優先使用されます。`build_rulebook.py` がルビとノイズを決定的に取り除くため、ルールブックには通常不要です。`--apply` で置き換えた行の `rag_chunks_opt` も削除されます。

```bash
# 状況確認
uv run ai-agents/optimize_chunks.py --status

# バッチ実行（システム未使用時間帯に実行推奨）
uv run ai-agents/optimize_chunks.py \
    --limit 50 \
    --model-url http://127.0.0.1:11441 \
    --embed-url http://127.0.0.1:11455
```

### 3.4 チャンクを無効化・有効化する

表紙・目次・奥付・記入用シートなどのノイズは `build_rulebook.py` が取り込み時に除外するため、手作業での無効化は通常不要です。特定のチャンクを RAG 検索から外したいときだけ無効化します。

- UI のルール画面 → 対象チャンクの「有効 / 無効」トグル（`enabled`）。各行には部 > 章 > 見出しと本のページが表示されます
- サンプルシナリオ「蛮族を駆逐せよ」の本文（tag `scenario`）は、GM がシナリオの筋書きをセッションに持ち込まないよう、無効の状態で取り込まれます。このシナリオを遊ぶときはルール画面で有効にします

```bash
# SQL で操作する場合（例: サンプルシナリオをまとめて有効化）
sqlite3 data/omni.db \
  "UPDATE rag_chunks SET enabled = 1 WHERE source_type='rulebook' AND tag='scenario';"
```

> 無効化の状態はチャンクの行に保存されるため、`build_rulebook.py --apply` で作り直すと初期状態（サンプルシナリオのみ無効）に戻ります。

### 3.5 ハウスルール・訂正を追加する

ルール画面（UI）から追加するか、直接 API を呼び出します。UI / API で追加・編集したルールはその場で埋め込まれ、すぐに RAG 検索の対象になります。ハウスルール・訂正は検索の順位で優先度のボーナスを受けるため、似た内容のルールブック原文より上に来ます。埋め込みに失敗した場合もルールは保存されるので（`POST` のレスポンスが `"embedded": false`）、mlx-serve を起動してから `embed_chunks.py` を実行してください。

```bash
# UI を使う場合: ルール画面 →「＋ ハウスルール・訂正を足す」、または各行の「訂正を作る」

# API を使う場合
curl -X POST http://localhost:8080/api/rules \
  -H 'Content-Type: application/json' \
  -d '{
    "source_type": "houserule",
    "tag": "combat",
    "text": "このキャンペーンでは精神抵抗力に+2のボーナスを適用する"
  }'
```

`overrides_id` に元チャンクの ID を指定した訂正・ハウスルールが有効な間は、元チャンクは RAG 検索から外れ、訂正側だけが使われます。訂正を無効にすると元チャンクが再び検索対象になります。

ゲーム中にどのルールが引かれるかは、ルール画面の検索（`GET /api/rules/search?q=...`、ゲーム中と同じ検索）で確かめられます。用語の読みで当たったチャンクには、当たった用語が表示されます。

---

## 4. NPC ペルソナ管理

### 4.1 新しいペルソナを追加する

`data/personas/{id}.yaml` を作成します。`id` フィールドはファイル名（拡張子なし）と一致させてください。

```yaml
id: new_npc
name: キャラクター名
race: 人族
classes:
  - name: ソーサラー
    level: 3
hp: 20
hp_max: 20
mp: 30
mp_max: 30
personality:
  tone: 冷静で分析的
  speech_style: 敬語・学術的な言い回し
  values: 古代魔法の解明
backstory: 魔術師ギルドの研究員。
action_rules:
  - 遠距離から魔法攻撃を行う
  - 近接戦闘には参加しない
```

LLM に渡るキー（`personality` の `tone` / `speech_style` / `values` / `fears` / `quirks`、`backstory`、`action_rules`、`equipment`、`skills` など）は [spec_ja.md](spec_ja.md#84-セッションの記憶) を、書式の全体は `data/personas/example_gard.yaml` を参照してください。

追加後、セッションを開いた状態で UI のパーティ画面からペルソナを選び「仲間に加える」で追加できます（1 セッション 3 体まで）。

### 4.2 ペルソナを更新する

ペルソナ YAML はセッションに加えるときに読み込まれ、NPC シートに写されます。ファイルを編集すると、その後に加えた NPC に反映されます（再起動は不要）。すでにセッションにいる NPC は変わりません。

セッションにいる NPC を直すとき:
1. パーティ画面 → 対象 NPC を選択 →「✎ 編集」。値だけを書き換え、YAML のほかのキーは保たれます
2. キーそのものを直すときは、ヘッダーの「DEV」をオンにして「YAML を直接編集 (DEV)」を使います（キーを消すとペルソナが失われるので注意）

NPC のペルソナはセッション固定の前置きに入るため、編集すると次のターンから反映されます。

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
    --comfy-url http://<COMFY_HOST>:8188 \
    --publish-quest
```

モデルは自動検出されます。手動で指定する場合:

```bash
uv run ai-agents/generate_scenarios.py \
    --comfy-url http://<COMFY_HOST>:8188 \
    --comfy-model "v1-5-pruned-emaonly.safetensors"
```

### 5.3 共通背景画像の事前生成

宿屋・ギルド・森など 8 種類の共通背景を一括生成します。複数シナリオで再利用されます。

```bash
uv run ai-agents/generate_scenarios.py \
    --backgrounds \
    --comfy-url http://<COMFY_HOST>:8188
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
    --comfy-url http://<COMFY_HOST>:8188
```

### 5.5 生成状況確認

```bash
uv run ai-agents/generate_scenarios.py --status
```

---

## 6. プレイヤーキャラクター管理

### 6.1 PC を作成・更新する

UI のパーティ画面 →「＋ PC を作る」から作成します。「使う」にした PC（全体で 1 人）のシートの要点が、セッション固定の前置きに入ります。

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

どこかのセッションに止まったターンがある間は、PC の更新（`PUT`）・削除・有効化・無効化が 409 になります（作成はできます）。

---

## 7. DEV モードの使い方

DEV モードはゲームが正常進行しない緊急時の根本対処用ツールです。**セッション進行中は慎重に使用してください。**

### 7.1 有効化

UI ヘッダー右端の「DEV」ボタンをクリック。注意のバナーが表示されます。

### 7.2 NPC YAML 直接編集（パーティ画面）

DEV ON 時、各 NPC の詳細に「YAML を直接編集 (DEV)」ボタンが表示されます。YAML を編集して保存するとDBに反映されます。構文エラーがある場合は保存が拒否されます。

### 7.3 ターンを取り消す・チェックポイントを消す

ターンの取り消しは DEV モードを使わず、セッション画面で行います。

- ⟲ このターンの前に戻す（またはサイドパネルのチェックポイントの「戻す」）: そのターンの開始前の状態に戻し、以降のターンを消す。NPC の HP/MP・配置も戻る
- ↻ 作り直す / ✎ 入力欄に戻して直す: 最後のターンをやり直す
- ⏹ 止める: 実行中のターンを止める。終わった段までは残り、「▶ 続きから」で再開するか「取り消す」で無かったことにする（[1.4](#14-止まったターンを続ける取り消す)）

チェックポイントはセッションごとに最新 10 件だけ残ります。個別に消す UI はありませんが、API で削除できます。

```bash
curl -X DELETE http://localhost:8080/api/checkpoint/5
```

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
# mlx-serve の起動状況確認
curl http://127.0.0.1:11441/health    # チャットモデル
curl http://127.0.0.1:11455/health    # 埋め込み
pwsh ~/AI/MLX/scripts/Get-MlxServeStatus.ps1

# 止まっているプロファイルを起動
pwsh ~/AI/MLX/scripts/Start-MlxServe.ps1 -Profile gemma4-31b-it-heretic-4bit-mtp
pwsh ~/AI/MLX/scripts/Start-MlxServe.ps1 -Profile bge-m3-q8

# オーケストレーターの起動時チェック結果
grep mlx-serve logs/orchestrator.log
```

- `LLM_MODEL_ID` / `EMBED_MODEL_ID` が mlx-serve のプロファイル名と一致しているか確認してください。mlx-vlm は `model` がないと 422 を返し、別の名前を送ると常駐モデルを再ロードします。
- ターンの途中で LLM が止まった場合、そのターンは止まったターンとして残ります。mlx-serve を起動し直してから、セッション画面の「▶ 続きから」で再開します（[1.4](#14-止まったターンを続ける取り消す)）

### 「止まったターンがあります」と言われる（409）

そのセッション（PC の操作ではどこかのセッション）に止まったターンが残っています。メッセージに出たセッションを開き、「▶ 続きから」か「取り消す」を選んでください（[1.4](#14-止まったターンを続ける取り消す)）。

### NPC が行動しない / HP・MP がそのまま適用される

段ごとの検証に通らなかった場合、`TurnResult` の `warnings` に理由が入ります（NPC はそのターンを見送り、Support が NG なら NPC 自身の `hp_delta` / `mp_cost` を使用）。`stage_attempts` で段ごとの呼び出し回数を確認できます。

```bash
# 却下された出力とエラーを確認
sqlite3 data/omni.db \
  "SELECT turn, role, substr(content,1,200) FROM session_logs WHERE content LIKE '[NG %' ORDER BY id DESC LIMIT 20;"
```

再思考の回数は `config.env` の `STAGE_RETRY_LIMIT`（既定 2）で調整できます。

### RAG 検索が機能しない（埋め込みがない・次元が合わない）

```bash
# 埋め込み未生成チャンクを確認
sqlite3 data/omni.db \
  "SELECT count(*) FROM rag_chunks WHERE embedding IS NULL AND enabled=1;"

# 埋め込みを生成
uv run ai-agents/embed_chunks.py \
    --embed-url http://127.0.0.1:11455

# 埋め込みモデルを変えた後（旧ベクトルと次元が違う）は全件作り直す
uv run ai-agents/embed_chunks.py --reembed-all
```

### ターン処理がタイムアウトする / 「処理中です」と言われる

- ターンの送信はすぐ 202 が返り、UI は WebSocket の進行イベントで待ちます。UI 側のタイムアウトはありません
- 1 ターン全体の上限: `config.env` の `TURN_TIMEOUT_SEC`（既定 900秒）。超えるとターンは止まり、終わった段までが残ります。「▶ 続きから」で再開すると、上限は数え直しになります
- LLM 1 リクエストのタイムアウト: `LLM_TIMEOUT_SEC`（既定 300秒）
- 全段が 1 モデルで直列に動くため、ターン時間は GM + NPC 数 + Support の合計になります。再思考が起きるとその分長くなります（`stage_attempts` で確認）。数ターンごとにあらすじの更新が続きます
- 時間を優先する場合は `STAGE_RETRY_LIMIT` を減らします。Gemma プロファイルに推奨の APC 設定（§1.1）が入っているかも確認します
- 1 セッションで同時に走るターンは 1 つです。実行中（あらすじの更新中を含む）に送ると 409「このセッションではターンを処理中です」になります。終わるまで待つか、⏹ 止めるで止めます（止めたターンは続きから再開できます）

### シーン画像が生成されない

```bash
# 稼働状態（images.ok）を確認
curl http://localhost:8080/api/system

# 画像工房の確認
curl http://127.0.0.1:11462/api/health

# 失敗の理由
grep "\[image\]" logs/orchestrator.log
```

- 画像工房（mlx-studio-web）と、構図を決める Gemma（mlx-serve）の両方が起動している必要があります
- 描くのは行動ターンの 1 ターン目と、`IMAGE_EVERY`（既定 3）の倍数のターンだけです
- 画像なしで進めたい場合は `config.env` で `IMAGE_EVERY=0` にします（`IMAGES_URL` を空にしても既定値に戻るため無効になりません）

### DB ロックエラー

```bash
# WAL チェックポイントを実行
sqlite3 data/omni.db "PRAGMA wal_checkpoint(FULL);"

# -shm / -wal ファイルを削除（全サービス停止後）
pwsh scripts/Stop-Omni.ps1
rm -f data/omni.db-shm data/omni.db-wal
pwsh scripts/Start-Omni.ps1
```

### Start-Omni.ps1 が「mlx-serve で次のプロファイルを起動してから」と表示して終了する

`LLM_URL` または `EMBED_URL` の `/health` が応答していません。表示された `Start-MlxServe.ps1` コマンドでプロファイルを起動してから、もう一度実行してください。

### Start-Omni.ps1 が「前回の .pids を検出」と表示して終了する

前回の停止が完了していません。`pwsh scripts/Stop-Omni.ps1` を実行してから起動してください。

---

## 9. 定期メンテナンス推奨スケジュール

| 頻度 | 作業 | コマンド |
|------|------|---------|
| 随時 | 不要なセッションの削除 | UI の開始画面「続きから」の 🗑 |
| 全データの作り直し時 | DB リセット | `./setup.sh` |
| セッション前 | 最新クエスト確認 | UI のクエスト画面 |
| 月1回 | シナリオ事前生成 | `generate_scenarios.py --count 5 --publish-quest` |
| 月1回 | DB バックアップ | `sqlite3 data/omni.db ".backup 'data/backup/omni_backup.db'"` |
| ルールブック更新時 | チャンク再構築 | `build_rulebook.py` → `build_rulebook.py --apply` → `embed_chunks.py` |
| 随時 | ハウスルール追加 | ルール画面 →「＋ ハウスルール・訂正を足す」 |

---

## 10. サービスポート一覧

| サービス | ポート | 説明 |
|----------|--------|------|
| Vite UI | 5173 | フロントエンド開発サーバー |
| Go Orchestrator | 8080 | REST API + WebSocket |
| mlx-serve `gemma4-31b-it-heretic-4bit-mtp` | 11441 | GM・NPC・サポート共通のチャットモデル（mlx-serve が管理） |
| mlx-serve `bge-m3-q8` | 11455 | ベクトル埋め込み生成（mlx-serve が管理） |
| mlx-studio-web | 11462 | MLX の画像工房。シーン画像（オプション、MLX 側が管理） |
| MLX ダッシュボード | 11470 | mlx-serve のプロファイルの起動・停止 |
| ComfyUI | 8188 | `generate_scenarios.py` の画像生成（Windows 側） |
