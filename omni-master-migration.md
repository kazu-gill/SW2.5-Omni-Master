# SW2.5-Omni-Master mlx-serve 移行・単一モデル直列化 計画

- 作業ルート: `/Users/kf/AI/SW2.5-Omni-Master/`
- LLM 基盤: mlx-serve (`/Users/kf/AI/MLX/`)。Omni-Master はクライアントの 1 つで、推論プロセスを一切起動・管理しない
- 作成日: 2026-09-27
- 後続の計画: MLX 基盤に合わせた再チューニングと UI 改修は [docs/plan-retune-20261112.md](docs/plan-retune-20261112.md)

---

## 1. 方針 (確定事項)

| 項目 | 決定 |
|---|---|
| 推論モデル | **Gemma 4 31B-it 1 本に GM / NPC / Support を一任**。複数モデルの常時ロードは廃止 |
| プロファイル | `gemma4-31b-it-heretic-4bit-mtp` (mlx-vlm + MTP、`:11441`、約 22GB)。単発 24.1 tok/s |
| 処理形態 | 並列 → **直列**。GM → NPC-A → NPC-B → NPC-C → Support の 5 段を順に呼ぶ (段数は減らさない) |
| 失敗時 | 各段の出力を検証し、NG なら**その段だけ再思考**させる (§4) |
| 埋め込み | nomic-embed (768 次元) → **`bge-m3-q8`** (llama、`:11455`、1024 次元)。`rag_chunks` / `rag_chunks_opt` を全件再埋め込み |
| 構造化出力 | MTP では `response_format` が 500 になるため使わない。プロンプト指示 + Go 側検証 + 再試行で担保 |

常駐メモリ: MTP 22GB + bge-m3 約 1GB → mlx-serve の `memory_limit_gb = 48` に収まる。

### 目標構成

```
mlx-serve (/Users/kf/AI/MLX, 別リポジトリ・変更しない)
  ├─ :11441  gemma4-31b-it-heretic-4bit-mtp  ← GM / NPC x3 / Support / バッチツール
  └─ :11455  bge-m3-q8                       ← RAG 埋め込み

SW2.5-Omni-Master
  Go Orchestrator :8080 ── LLM_URL + LLM_MODEL_ID / EMBED_URL + EMBED_MODEL_ID
  UI :5173
```

---

## 2. mlx-serve 側の制約 (docs/verification-20260925.md・docs/endpoints.md より)

| 制約 | Omni-Master 側の対応 |
|---|---|
| mlx-vlm は `model` 必須 (省略で 422)。別の名前を送ると**常駐モデルを破棄して再ロード** | 全リクエストに `model_id` を必ず付ける。空なら起動時エラーにする |
| mlx-vlm は `seed` 未指定だと固定 seed で同じ文を返す | NPC 3 体が同じ入力に似た反応をしないよう、毎リクエストでランダムな `seed` を送る |
| thinking の既定は無効 (mlx-vlm)。有効化はトップレベル `enable_thinking=true` | 通常時は `{"chat_template_kwargs":{"enable_thinking":false},"enable_thinking":false}` を送る。再思考時のみ有効化 (§4) |
| 推論テキストは `reasoning_content` に分離される | `content` だけを使う。`reasoning_content` はログにだけ残す |
| MTP 有効時は `response_format` 不可 | 使わない |
| 埋め込みは `/v1/embeddings` (OpenAI 互換) | Go / Python とも `/embedding` から `/v1/embeddings` に変更 |

---

## 3. 現状コードとの差分 (調査結果)

| 箇所 | 現状 | 変更 |
|---|---|---|
| `orchestrator/pkg/llm/client.go` `Complete` | `model` なし、`seed` なし、thinking 指定なし | `model` / `seed` / thinking フラグを追加。`reasoning_content` と `finish_reason` も受け取る |
| `client.go` `Embed` | `/embedding` `{"content"}` | `/v1/embeddings` `{"model","input"}` → `data[0].embedding` |
| `client.go` `CompleteParallel` | 未使用の並列ヘルパー | 削除 |
| `client.go` タイムアウト | 120 秒固定 | `LLM_TIMEOUT_SEC` で設定可能にする (既定 300)。GM `max_tokens=2048` を 15〜24 tok/s で出すと最悪 85〜140 秒かかる |
| `pkg/game/turn.go` `broadcastNPCs` | goroutine で 3 体を並列、クライアントは `NPCs[idx%3]` | for ループで直列。クライアントは 1 つ |
| `turn.go` `calculateResources` | JSON パース失敗時に黙って NPC の値へフォールバック | 検証 + 再思考を入れ、それでも失敗したときだけフォールバック (§4) |
| `turn.go` `gmNarrate` | 検証なし | 検証 + 再思考を追加 (§4) |
| `pkg/game` `LLMPool` | GM / NPCs[3] / Support の 5 クライアント | `LLM *llm.Client` 1 つにまとめる (段ごとの温度・`max_tokens` は呼び出し側に残す) |
| `pkg/config/config.go` | `GM_URL` など 6 つの URL | `LLM_URL` `LLM_MODEL_ID` `EMBED_URL` `EMBED_MODEL_ID` `LLM_TIMEOUT_SEC` に置き換える |
| `config.env.example` | `LLAMA_SERVER` / `MODEL_*` / 6 つの URL | 上記キーに置き換える |
| `ai-agents/generate_scenarios.py` / `optimize_chunks.py` `call_llm` | `/completion` (生プロンプト) | `/v1/chat/completions`。プロンプトは user メッセージにし、`model` / `seed` / thinking 無効化を付ける。`stop` / `top_p` / `temperature` は残す |
| `ai-agents/embed_chunks.py` / `optimize_chunks.py` 埋め込み | `/embedding` | `/v1/embeddings` + `model` |
| `ai-agents/embed_chunks.py` 既定 URL | `http://localhost:11431` (Support のポートで**既存バグ**) | 既定を `:11455` にする |
| Python の既定 URL (`:11430` / `:11435`) | 旧ポート | `:11441` / `:11455`、`--model-id` / `--embed-model-id` を追加 |
| `start.sh` / `stop.sh` | llama-server 6 プロセスを起動・停止 | §5 Phase 4 で PowerShell に置き換える |
| `data/schema.sql` | どこからも参照されていない古いコピー (`UNIQUE(session_id,name)` や `rag_chunks.tag/enabled/overrides_id` がない)。正は `go:embed` している `orchestrator/pkg/db/schema.sql` | 削除する |
| README / docs | 6 プロセス構成・Gemma-4B/2B の記述 | 新構成に更新 |
| ランタイムの pin | `mise.toml` なし、`ui/package.json` に Volta なし | `mise.toml` (Go) と `volta` (Node) を追加 |

---

## 4. 段ごとの検証と再思考

処理の流れ (5 段・直列) は変えない。各段で出力を検証し、NG ならその段だけを再度呼ぶ。

### 共通の仕組み

`pkg/game` に次の形の小さなヘルパーを作り、3 種類の段で共通に使う:

```go
// runStage は検証に通るまで LLM を呼び直す。
// 1 回目: thinking なし。2 回目以降: 前回の出力とエラー内容を会話に足し、thinking ありで呼び直す。
func (e *Engine) runStage(ctx, stage string, msgs []llm.Message, opts llm.CompletionOptions,
    validate func(text string) error) (text string, attempts int, err error)
```

- 上限は `STAGE_RETRY_LIMIT` (既定 2、つまり最大 3 回)
- **再思考** = 2 回目以降に `enable_thinking=true` を送り、`max_tokens` を思考分だけ増やす (+2048)。前回の出力とエラー文も渡す
- 各試行を `session_log` に `role=<stage>`、試行番号・エラー・`reasoning_content` の有無付きで残す
- ターン結果に段ごとの試行回数を載せ、UI の SystemConsole に「NPC-B: 再思考 1 回」のように表示する (任意)

### 段ごとの NG 条件

| 段 | NG 条件 | 上限を超えたとき |
|---|---|---|
| GM | 本文が空 / `finish_reason == "length"` (途中で切れた) / 定型の拒否文 / 他 NPC の YAML・JSON を出している | ターンを失敗にし、チェックポイントは残したままエラーを返す (今と同じ) |
| NPC (x3) | 今の `ParseNPCAction` + `ValidateNPCAction` に加えて、`name` がシート名と一致しない / `dialogue` が空 / `new_lane` の値が不正 | その NPC だけ欠落として続ける (今と同じ)。ほかの NPC は処理を続ける |
| Support | JSON 配列としてパースできない / 行動した NPC の名前がすべて揃っていない / 知らない名前がある / 値が各 NPC の HP・MP の範囲外 | 今のフォールバック (NPC 自身の `hp_delta` / `mp_cost` を使う) を行い、ログに `fallback` と残す |

> 検証関数は `validate.go` に置き、LLM を使わない単体テストを書く (TDD)。

### 要検証 (mlx-serve 側で未確認)

| ID | 内容 | だめだったとき |
|---|---|---|
| R1 | MTP プロファイルで `enable_thinking=true` が効き、思考が `reasoning_content` に分かれるか (V8 は MTP で未実施) | 再思考は「thinking なし + エラー文を渡して再生成」だけにする |
| R2 | MTP で `seed` が効くか (同じ入力・違う seed で違う出力になるか) | `temperature` だけで多様性を出す。NPC ごとにプロンプトへ個性の差を足す |

---

## 5. フェーズ

各フェーズの完了時に `go test ./...`、`uv run ruff format . && uv run ruff check .` (ai-agents)、`npm run lint` (ui) を通す。

### Phase 0 — 前提の確認 (コード変更なし)

1. mlx-serve で `gemma4-31b-it-heretic-4bit-mtp` と `bge-m3-q8` を起動し、`Get-MlxServeStatus.ps1` で両方が稼働していることを確認
2. R1 / R2 を curl で確認し、結果を `docs/mlx-serve-client-notes.md` に記録
3. `/v1/embeddings` のレスポンス形式と次元数 (1024) を確認
4. 今の `data/omni.db` をバックアップ (`data/omni.db.bak-<日付>`)

### Phase 1 — LLM クライアントの刷新 (`pkg/llm`)

1. `client_test.go` に先にテストを足す (httptest サーバで送信 JSON を検査):
   - `model` が必ず入る / `seed` が毎回入る / thinking 無効化フラグが入る / `Thinking: true` で `enable_thinking=true`
   - `finish_reason` と `reasoning_content` を返す
   - `Embed` が `/v1/embeddings` に `{"model","input"}` を送り、`data[0].embedding` を返す
2. `New(baseURL, modelID string, timeout time.Duration)` に変更。`Complete` の戻り値を `Result{Text, Reasoning, FinishReason}` に変更
3. `CompleteParallel` を削除

### Phase 2 — 設定と配線 (`pkg/config`, `cmd/server`)

1. `Config` を `LLMURL` / `LLMModelID` / `EmbedURL` / `EmbedModelID` / `LLMTimeoutSec` / `StageRetryLimit` に変更。`LLMModelID` / `EmbedModelID` が空なら `Load` がエラーを返す
2. 既定値: `http://127.0.0.1:11441` / `gemma4-31b-it-heretic-4bit-mtp` / `http://127.0.0.1:11455` / `bge-m3-q8` / 300 / 2
3. `config.env.example` を更新 (旧キーは削除し、移行メモをコメントで残す)
4. 起動時に `LLM_URL/health` と `EMBED_URL/health` を確認し、応答がなければ「mlx-serve で起動すべきプロファイル名」をログに出す (起動は止めない)

### Phase 3 — ターン処理の直列化と再思考 (`pkg/game`)

1. `validate.go` に `ValidateGMNarration` / `ValidateNPCActionFor(sheet)` / `ValidateResourceDeltas(actions, sheets)` を追加 (テスト先行)
2. `runStage` を実装 (テスト: 偽の LLM で「1 回目 NG → 2 回目 OK」「全部 NG」「2 回目以降だけ thinking あり」を確認)
3. `LLMPool` を 1 クライアントにまとめ、`gmNarrate` / `broadcastNPCs` (for ループ化) / `calculateResources` を `runStage` 経由にする
4. `turn_test.go` を更新: 呼び出し順が GM → NPC-A → B → C → Support になっていること、1 体だけ失敗したときに残りが続くこと
5. ターン結果に `StageAttempts` を追加 (UI での表示は任意)

### Phase 4 — 起動・停止スクリプト

1. `scripts/Start-Omni.ps1` / `scripts/Stop-Omni.ps1` を新しく作る (PowerShell 7、LF、UTF-8 BOM なし)
   - `config.env` を読み、`LLM_URL` / `EMBED_URL` のヘルスを確認する。未起動なら `pwsh /Users/kf/AI/MLX/scripts/Start-MlxServe.ps1 -Profile <名前>` を**表示して終了**する (mlx-serve を勝手に起動しない)
   - DB がなければ初期化 → Go のビルド・起動 → Vite の起動。PID は今の `.pids` 方式を使う
2. 新しいスクリプトで Phase 6 の通し実行が通ったら、`start.sh` / `stop.sh` を削除する (削除はユーザーの確認を取ってから)

### Phase 5 — バッチツールと再埋め込み (`ai-agents`)

1. `call_llm` (2 ファイル) を `/v1/chat/completions` に書き換える。`ping` は `/health` のままでよい (mlx-vlm は常駐モデルのロード後に 200)
2. 埋め込み (2 ファイル) を `/v1/embeddings` に書き換える。既定 URL・model ID を新しくし、`embed_chunks.py` の既定ポートのバグを直す
3. `embed_chunks.py` に `--reembed-all` を追加 (`embedding` を NULL に戻してから全件処理。`rag_chunks_opt` も対象)
4. Phase 0 のバックアップがあることを確認してから、全件を再埋め込みする
5. 確認: 代表的な 10 クエリ (例: 「先制判定」「魔法の達成値」「転倒」) で上位 5 件を旧 DB と見比べ、`docs/rag-bge-m3-check.md` に記録する

### Phase 6 — 通し検証

| ID | 内容 | 合格基準 |
|---|---|---|
| O1 | 5 ターンの通し実行 × 2 セッション | エラーで止まらない |
| O2 | 段ごとの初回成功率と、再思考で回復した率 (ログから集計) | Support の最終的な JSON 成功率 ≥ 19/20。フォールバックは 1 回以下 |
| O3 | NPC 3 体の出力に他 NPC の名前・台詞が混ざらないか | 混入 0 |
| O4 | NPC の多様性 (同じ場面で 3 体の `action` がすべて同じにならない) | 5 ターン中 4 ターン以上で差がある |
| O5 | 1 ターンの所要時間 (段ごとの内訳付き) | 記録するだけ。目標値は計測してから決める |

結果は `docs/verification-serial-<日付>.md` に記録する。旧構成 (llama-server 6 プロセス) の比較データはこのマシンにないため、比較はせず、この計測を新しい基準にする。

### Phase 7 — 後片付け

1. 使われていない `data/schema.sql` を削除する (§3)
2. `mise.toml` (Go 1.26.x) と `ui/package.json` の `volta` を追加
3. README (構成図・前提・設定リファレンス・起動手順) と `docs/*_ja.md` / `*_en.md` を新しい構成に更新

---

## 6. 規約

- `config.env` のパスはすべて絶対パスで書く
- スクリプトは PowerShell 7 (`.ps1`、LF、UTF-8 BOM なし)
- ランタイム: Python は uv、Node は Volta、Go は Mise
- **mlx-serve のリポジトリは変更しない**。不具合や足りない機能を見つけたら報告だけする
- コミットはフェーズごとに分ける

### 止めてユーザーの判断を仰ぐこと

1. R1 / R2 の結果が「だめ」で、§4 の代わりの方法も使えない
2. `data/omni.db` のスキーマ変更が必要になった (今の計画では不要)
3. 再埋め込み後の RAG 確認で、明らかに検索結果が悪くなった
4. モデルを新しくダウンロードする必要が出た

## 7. 各フェーズ完了時の報告

- 何をしたかと、完了条件を満たしたか
- テスト・lint の結果
- 段ごとの試行回数と所要時間 (Phase 3 以降)
- 止まった理由と、未解決のこと

---

## 8. 進捗 (2026-09-27)

| Phase | 状態 | メモ |
|---|---|---|
| 0 | ✅ | R1 (MTP で thinking) / R2 (seed) とも OK。`docs/mlx-serve-client-notes.md` |
| 1〜3 | ✅ | Go 側。1 モデル直列化 + 段ごとの検証・再思考。`go test ./...` 通過 |
| 4 | ✅ | `scripts/Start-Omni.ps1` / `Stop-Omni.ps1`。`start.sh` / `stop.sh` は削除済み |
| 5 | ✅ | バッチツールを mlx-serve 対応。旧環境の DB を移植し、ルールブックのチャンクを原本から作り直して埋め込んだ (`docs/rulebook-rebuild-20260927.md`) |
| 6 | ✅ | `docs/verification-serial-20260927.md`。中央値 19 秒/ターン、再思考 0 回 |
| 7 | ✅ | `data/schema.sql` 削除、`mise.toml` / Volta pin、README・docs 更新 |

lint について: `ai-agents` は既存コードに ruff の指摘が元から約 40 件あり、`ui` は `npm run lint` で既存の 6 件 (react-hooks) が出る。今回触ったファイルで新たな指摘は増やしていない。一括整形は差分が大きくなるため別作業とする。

## 9. 次の改善候補 (未着手・要判断)

Phase 6 で見えた課題。処理の流れ (5 段直列) は変えずにできる。

1・2 (ペルソナ、先の NPC の行動) は対応済み。残りを含む現在の計画は [docs/plan-retune-20261112.md](docs/plan-retune-20261112.md) を参照。

1. **NPC にペルソナを渡す** — 今は名前・HP・MP・レーンしか渡していない。`npc_sheets.yaml_blob` の種族・職業・性格・口調をシステムプロンプトに入れる。口調の重なりや、魔法職が「盾で押し返す」といったズレが減る見込み
2. **後の NPC に先の NPC の行動を渡す** — 直列化したので、同じターンで先に行動した NPC の `action` / `dialogue` を後の NPC のプロンプトに足せる。行動の重複 (2 人とも「罠の解除」) を避けられる
3. **Support の役割を見直す** — 現状は NPC の申告値をそのまま写すだけ (10/10 一致)。ルール (RAG) を参照して妥当性を判定させるか、段を残したまま Go 側の計算に寄せるかを決める
4. **mlx-serve の散発的な停滞 (約 40 秒) の調査** — mlx-serve 側の課題。MTP なしのプロファイルでも起きるかを確認する (mlx-serve リポジトリ側の作業)
