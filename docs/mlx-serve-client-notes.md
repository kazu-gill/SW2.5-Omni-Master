# mlx-serve クライアント検証メモ (2026-09-27)

Omni-Master から mlx-serve (`~/AI/MLX`) を使うときに確認した挙動。エンジン共通の検証は mlx-serve の `docs/verification-20260925.md` を参照。

## 常駐構成

| profile | engine | port | 用途 |
|---|---|---|---|
| `gemma4-31b-it-heretic-4bit-mtp` | mlx-vlm + MTP | 11441 | GM / NPC x3 / Support / バッチツール (チャット全部) |
| `bge-m3-q8` | llama | 11455 | RAG 埋め込み |

推定メモリ合計 23.3 GB。チャット用モデルは 1 本だけ常駐させ、直列で呼ぶ。

- 並列で呼ばないので、MTP の効果が最も出る (単発 24.1 tok/s、MTP なしの約 1.57 倍)
- チャットモデルを 2 本 (MTP 版 + JSON Schema 用の MTP なし版) 常駐させると約 40 GB になり、ほかのアプリの余地がなくなる。構造化出力は Go 側の検証と再思考で担保する
- 埋め込みはチャットモデルでは代用できないため、別に常駐させる

## R1: MTP 版での thinking — OK

| リクエスト | 結果 |
|---|---|
| `"enable_thinking": true` | `content` は答えだけ。思考は `message.reasoning_content` に分離される。今回の例では 181 tokens (思考込み) |
| `"enable_thinking": false` + `"chat_template_kwargs": {"enable_thinking": false}` | `reasoning_content` は null |

→ 再思考 (2 回目以降の試行) で `enable_thinking: true` を使える。思考分のトークンも `max_tokens` を消費するため、再思考時は `max_tokens` を増やす。

## R2: seed — 指定が必須

同じプロンプト、`temperature: 0.8` での結果:

| 条件 | 結果 |
|---|---|
| seed なし × 2 | 完全に同じ文 |
| `seed: 1` × 2 | 完全に同じ文 (seed なしとは違う) |
| `seed: 2` / `seed: 3` | それぞれ違う文 |

→ seed を送らないと、temperature を上げても毎回同じ文になる。NPC ごと・試行ごとにランダムな `seed` を送る。

## 埋め込み

`POST /v1/embeddings` `{"model": "bge-m3-q8", "input": "<text>"}` → `data[0].embedding` (1024 次元)。

## プレフィックスキャッシュ (APC) — 2026-10-05

Omni-Master は 1 ターンの全段を 1 本の会話として送る (`docs/spec_ja.md` §8.2)。前の段のリクエストがそのまま次の段の先頭になるので、mlx-vlm の APC で prefill を省ける。ただし mlx-serve の既定では APC は無効。

Gemma 4 はスライディングウィンドウ層を含むため、APC はチェックポイントの位置でしか KV を復元できない。チェックポイントは 2048 トークンごとと、プロンプト末尾 (`APC_CHECKPOINT_GUARD_TOKENS` 手前) に置かれる。生成プロンプトの末尾 `<|turn>model\n<|channel>thought\n<channel|>` は、次のリクエストで履歴としてレンダリングされると `<|turn>model\n<応答>` になり、末尾の 4 トークンが一致しない。既定のガード (1 トークン) ではチェックポイントが分岐点より後ろになり、2048 トークン単位でしかヒットしない。

mlx-serve 側で必要な設定 (`gemma4-31b-it-heretic-4bit-mtp` の起動環境):

| 環境変数 | 値 | 理由 |
|---|---|---|
| `APC_ENABLED` | `1` | APC を有効にする (既定 `0`) |
| `APC_CHECKPOINT_GUARD_TOKENS` | `8` | 末尾チェックポイントを `<|turn>model\n` の手前に置き、次の段で一致させる |
| `APC_CHECKPOINT_ENTRIES` | `4` | 常駐チェックポイント数 (既定 2)。段の切り替えで追い出されないように |

実行中のサーバーなら `PATCH /v1/settings` に `{"apc_enabled": true, "apc_checkpoint_guard_tokens": 8, "apc_checkpoint_entries": 4}` でも変えられる (モデルの再ロードが走る)。

計測結果は `docs/verification-apc-20261005.md`。
