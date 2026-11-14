# Orchestrator API (2026-11 改修後)

UI (React) と Go オーケストレーター (:8080) の取り決め。UI は `/api` と `/ws` を Vite のプロキシ経由で呼ぶ。
エラーはすべて `{"error": "<日本語の説明。次に何をすればよいかを含める>"}` と 4xx/5xx。

## ターンの流れ

ターンは時間がかかる (40〜60 秒) ので、**POST はすぐ 202 を返し、進行は WebSocket で届く**。
1 セッションで同時に走るターンは 1 つ。走っている間の POST は 409。

```
POST /api/sessions/{id}/turn  ─▶ 202 {"turn": 7}
WS  stage  gm start   ─▶ delta… ─▶ stage gm done
WS  rules  (GM が参照したルール)
WS  check  (判定があれば。コードが 2d6 を振った結果)
WS  stage  npc:ガルド start ─▶ delta… ─▶ done      (NPC の数だけ)
WS  stage  support start ─▶ done
WS  turn   (確定した TurnResult)
WS  stage  summary start/done   (数ターンごと。次のターンは待たされる)
WS  image  (数ターンごと、後から)
```

## REST

### システム
| メソッド | パス | 返り値 |
|---|---|---|
| GET | `/api/system` | `{"llm":{"url","model_id","ok"},"embed":{"url","model_id","ok"},"images":{"url","ok"},"apc_tenant"}` |

### セッション
| メソッド | パス | 本文 | 返り値 |
|---|---|---|---|
| GET | `/api/sessions` | | `[SessionSummary]` (更新が新しい順) |
| POST | `/api/sessions` | `{"title"?, "quest_id"?}` | `Session` (201) |
| GET | `/api/sessions/{id}` | | `SessionState` |
| DELETE | `/api/sessions/{id}` | | 204 |
| POST | `/api/sessions/{id}/turn` | `{"text", "mode": "action" \| "gm"}` | 202 `{"turn"}` / 409 実行中 |
| POST | `/api/sessions/{id}/cancel` | | 204 (実行中のターンを中断。終わった段までは残り、止まったターンになる) |
| POST | `/api/sessions/{id}/resume` | | 202 `{"turn"}`。止まったターンを止まった段から続ける / 404 止まったターンが無い / 409 実行中・パーティが変わった |
| POST | `/api/sessions/{id}/discard` | | 204。止まったターンを取り消し、そのターンの開始前に戻す / 404 止まったターンが無い / 409 実行中 |
| POST | `/api/sessions/{id}/redo` | `{"turn", "text"?}` | 202 `{"turn"}`。そのターン以降を消し、開始前の状態に戻してから (text があれば差し替えて) やり直す |
| POST | `/api/sessions/{id}/restore` | `{"turn"}` | 204。そのターンの開始前の状態に戻す (以降のターンは消える) |
| POST | `/api/sessions/{id}/quest` | `{"quest_id"}` | 204。受注 (セッションのクエストにする) |

`mode: "gm"` は GM 直訴。GM だけが答え、NPC と数値は動かない。

補足:
- **止まったターン**: 段 (GM・各 NPC・Support) が終わるたびに途中結果が DB に残る。その後の失敗・中断・時間切れでは開始前に戻さず、
  WS の `error` に `resumable: true` が付き、`SessionState.stopped_turn` に出る。「続きから」(`/resume`) か「取り消す」(`/discard`) を選ぶ。
  オーケストレーターを再起動しても残る (起動時に実行中だったターンは止まったターンになる)。
  途中結果ができる前の失敗 (チェックポイントの保存など) は今までどおり開始前に戻り、`resumable` は付かない
- **中断** (`/cancel`) の後は WS に `error` (「ターンを中断しました。途中まで残っています…」, `resumable: true`) と `busy: false` が届く
- **再開** (`/resume`) は新しいターンと同じく WS で進む (busy・時間切れも同じ)。止まった後に NPC の顔ぶれが変わっていると 409 (取り消すしかない)
- **busy** はターン確定後の要約 (`stage: summary`) が終わるまで true のまま。要約中も次の送信は 409
- **やり直し** (`/redo`) は元のターンのモード (行動 / 直訴) のまま。モードは変えられない
- **巻き戻し** (`/restore {turn}`) の `turn` は「そのターンの開始前」。チェックポイントは最新 10 件だけ残る

### 止まったターンがある間に断る操作 (409)

本文は `{"error": "止まったターンがあります。「続きから」か「取り消す」を先に選んでください"}`。

| 操作 | 断る条件 |
|---|---|
| `POST /api/sessions/{id}/turn` | そのセッションに止まったターンがある |
| `POST /api/sessions/{id}/redo` | 同上 (止まったターン自身を直すときは、取り消してから新しい入力で送る) |
| `POST /api/sessions/{id}/restore` | 同上 |
| `POST /api/sessions/{id}/quest` | 同上 |
| `PATCH /api/npc-position` | `session_id` のセッションに止まったターンがある |
| `POST` / `DELETE /api/npc-sheet` | 同上 |
| `PATCH /api/npc-sheet/{id}` | そのシートのセッションに止まったターンがある (シートが無ければ 404) |
| `PUT` / `DELETE /api/player-characters/{id}`、`PATCH .../activate`、`.../deactivate` | どこかのセッションに止まったターンがある (PC の作成は断らない)。本文は止まったセッションを名指しする: `セッション「<title>」(#<id>) に止まったターンがあります。そのセッションで「続きから」か「取り消す」を先に選んでください` |

グローバルなクエストの受注・完了 (`PATCH /api/quests/{id}/accept`、`.../complete`) は断らない。止まったターンが読むクエストはセッション側 (`sessions.quest_id`) だけなので影響しない。
`PATCH /api/npc-sheet/{id}` は、ターンが実行中のセッションでも 409 (ターン確定時に全シートを開始時の写しから書き直すため)。

### パーティ・PC・クエスト・ルール (既存を継続)
| メソッド | パス | 備考 |
|---|---|---|
| GET | `/api/personas` | `[{"id","name","hp","mp"}]` |
| POST | `/api/npc-sheet` | `{"session_id","persona_id"}`。1 セッション 3 体まで (超えると 400) |
| DELETE | `/api/npc-sheet` | `{"session_id","name"}` |
| PATCH | `/api/npc-sheet/{id}` | `{"hp","mp","yaml_blob"?}`。yaml_blob を送るときは **元の YAML のキーを保ったまま** 値だけ変えたものを送る |
| PATCH | `/api/npc-position` | `{"session_id","name","lane": "enemy"\|"front"\|"party"}` |
| GET/POST/PUT/DELETE | `/api/player-characters[/{id}]`、`PATCH .../{id}/activate` | 既存どおり |
| GET | `/api/quests` | 既存どおり |
| PATCH | `/api/quests/{id}/complete` | 既存どおり |
| GET | `/api/rules?q=&source_type=&limit=50&offset=0` | `{"total","items":[Rule]}`。q は本文・節の部分一致。空の q / source_type は絞り込みなし |
| GET | `/api/rules/search?q=&k=5` | `[RuleHit]`。ゲーム中と同じ検索 (埋め込み + 用語の完全一致) |
| POST/PATCH/DELETE | `/api/rules[/{id}]` | 既存どおり (保存時に埋め込む) |
| GET | `/api/sessions/{id}/images/{file}` | 生成画像 (png) |

## WebSocket `/ws?session_id={id}`

サーバーから送るだけ。すべて `type` と `session_id` を持つ。

| type | フィールド | 意味 |
|---|---|---|
| `stage` | `turn`, `stage` (`gm` / `npc:<名前>` / `support` / `summary` / `image`), `label` (表示名), `status` (`start` / `retry` / `done` / `failed` / `skipped`), `attempt`, `message`? | 段の開始・再思考・終了 |
| `delta` | `turn`, `stage`, `text` | 生成中の本文の追加分 (GM・NPC)。`retry` が来たらその段の本文を捨てて受け直す |
| `rules` | `turn`, `stage`, `rules: [RuleHit]` | その段に渡したルール |
| `check` | `turn`, `check: Check` | 判定の結果 |
| `turn` | `result: TurnResult` | ターン確定 |
| `error` | `turn`, `message`, `resumable` | ターンが失敗した。`resumable: true` なら途中まで残っていて `/resume` で続けられる。false なら状態は開始前に戻っている (再開中にパーティの変化が分かったときだけは止まったターンが残り、`/discard` で取り消す) |
| `image` | `turn`, `url` | シーン画像ができた |
| `busy` | `busy: bool` | ターンの実行状態 (他のタブ向け) |

## 型

```ts
Session        { id, title, quest_id: number|null, turn: number, created_at, updated_at }
SessionSummary { ...Session, quest_title: string, last_line: string,
                 stopped_turn: boolean }   // そのセッションに止まったターンがある
SessionState   { session: Session, quest: Quest|null,      // quest は GET /api/quests の要素と同じ形
                 pc: { id, name, is_active, sheet, json_blob }|null,  // 全体で 1 人の「使用中」PC。sheet は json_blob を解析したもの
                 npc_sheets: NPCSheet[], turns: TurnResult[], checkpoints: {turn, created_at}[],
                 summary: string[], busy: boolean,
                 stopped_turn: StoppedTurn|null }
StoppedTurn    { turn, input, mode: "action"|"gm", error: string,   // error は止まった原因
                 done: string[],          // 終わった段: "gm" / "npc:<名前>" / "support"
                 result: TurnResult }     // 終わった段までの結果 (GM の語り・行動した NPC・判定)
NPCSheet       { id, session_id, name, hp, mp, hp_max, mp_max, lane: "enemy"|"front"|"party", yaml_blob }
TurnResult     { session_id, turn, mode: "action"|"gm", input, gm_narration,
                 npc_actions: NPCAction[], deltas: ResourceDelta[], formation_changes: FormationChange[],
                 checks: Check[], rules_used: RuleHit[], stage_attempts: Record<string, number>,
                 warnings: string[], image_url?: string, created_at }
NPCAction      { name, action, dialogue, mp_cost, hp_delta, new_lane?, check?: string }
ResourceDelta  { npc_name, hp_delta, mp_cost }
FormationChange{ name, new_lane }
Check          { who, kind, base, target, dice: [number, number], total,
                 result: "success"|"failure"|"auto_success"|"auto_failure" }
RuleHit        { id, source_type, section, page_from, page_to, text, score, matched_term? }
```
