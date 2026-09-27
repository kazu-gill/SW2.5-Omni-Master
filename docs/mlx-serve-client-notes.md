# mlx-serve クライアント検証メモ (2026-09-27)

Omni-Master から mlx-serve (`/Users/kf/AI/MLX`) を使うときに確認した挙動。エンジン共通の検証は mlx-serve の `docs/verification-20260925.md` を参照。

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
