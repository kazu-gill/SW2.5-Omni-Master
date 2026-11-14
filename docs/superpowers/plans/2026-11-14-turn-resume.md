# ターンの途中からの再開 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 止まったターン (通信の失敗・中断・時間切れ・GM の再試行切れ・DB のエラー) を、終わった段を作り直さずに「続きから」再開できるようにする。

**Architecture:** 段が終わるたびに、ターンの途中結果 (終わった段・transcript・判定・TurnResult) を新しい表 `turn_progress` に保存する。`ResumeTurn` はそれを読んで `turnRun` を組み直し、残りの段だけを同じ道 (終わった段は飛ばす) で回す。ターンの締め (シートの更新・turn_results・progress の削除) は 1 つのトランザクションにまとめる。

**Tech Stack:** Go (orchestrator, `database/sql` + SQLite)、React + TypeScript (ui/)

**Spec:** `docs/superpowers/specs/2026-11-14-turn-resume-design.md`

## Global Constraints

- 読み出しは DB だけ。メモリに途中結果を持つ経路は作らない
- 振ったサイコロは再開で振り直さない (facts・Checks は保存したものを使う)
- 再開後の各段のリクエストは、止まらずに進んだ場合と同じメッセージ列にする (APC の prefix)
- 1 セッションに止まったターンは高々 1 つ (`turn_progress.session_id` が主キー)
- 自動の再試行・段の途中からの再開・mlx-serve 側への適用はしない
- 試験の実行: `cd orchestrator && go test ./...`。画面は `cd ui && npm run lint && npm run build`

## Review Focus

1. 隊列が変わる NPC の HP・MP の変化が両方とも残る. **今のコードの不具合**: `applyFormationChanges` は変化前の `sheets` から書き直すので、`applyDeltas` が書いた HP・MP を上書きして消す。締めを 1 つの関数にまとめるときに直す (Task 2 の試験)
2. 実行中・再開中に「続きから」をもう一度押す → 二重には走らない (`errBusy`, Task 4)
3. ターンの最中にオーケストレーターを落とす → 起動時に running が stopped になり、続きから再開できる (Task 1・4)
4. 再開中にもう一度中断する → それまでの段 + 再開後に終わった段が残り、さらに続きから再開できる (Task 3)
5. GM 段で止まった (終わった段がない) ターンを再開 → GM から始まり、player の行もチェックポイントも二重にならない (Task 3)

---

### Task 1: `turn_progress` の表と DB の関数

**Files:**
- Modify: `orchestrator/pkg/db/schema.sql` (表を追加)
- Create: `orchestrator/pkg/db/progress.go`
- Modify: `orchestrator/pkg/db/sessions.go` (`deleteTurnsFrom` が turn_progress も消す)
- Test: `orchestrator/pkg/db/progress_test.go`

**Interfaces:**
- Produces:
  ```go
  type TurnProgress struct {
      SessionID int64; Turn int; Input, Mode, StateJSON string
      Status string // "running" | "stopped"
      Error  string; UpdatedAt time.Time
  }
  const ProgressRunning, ProgressStopped = "running", "stopped"
  func SaveTurnProgress(d *sql.DB, p TurnProgress) error              // UPSERT, updated_at を更新
  func GetTurnProgress(d *sql.DB, sessionID int64) (*TurnProgress, error) // 無ければ nil, nil
  func MarkTurnStopped(d *sql.DB, sessionID int64, errMsg string) error
  func StopRunningTurns(d *sql.DB) (int64, error)                      // 起動時: running → stopped, 件数
  func AnyStoppedTurn(d *sql.DB) (bool, error)                         // 全体に効く編集 (PC・クエスト) の判定用
  func DeleteTurnProgress(d *sql.DB, sessionID int64) error
  func MaxLogID(d *sql.DB, sessionID int64) (int64, error)              // session_logs の最大 id (無ければ 0)
  func DeleteTurnLogsAfter(d *sql.DB, sessionID int64, turn int, afterID int64) error
  func FinishTurn(d *sql.DB, sessionID int64, turn int, sheets []NPCSheet, resultJSON string) error
  ```
  `FinishTurn` は 1 トランザクションで: `sheets` を `upsertNPCSheet` → turn_results に UPSERT → turn_progress を削除。

- [ ] **Step 1: 失敗する試験を書く** (`progress_test.go`)
  - `TestTurnProgressRoundTrip`: Save → Get で全項目が戻る。2 回目の Save で上書き (行は 1 つ)
  - `TestGetTurnProgressNone`: 無いセッションは `nil, nil`
  - `TestStopRunningTurns`: running 2 件・stopped 1 件 → 戻り値 2、全件 stopped
  - `TestMarkTurnStopped`: status=stopped、error が入る
  - `TestFinishTurnAtomic`: シートの HP が変わり、turn_results に行があり、progress が消える。
    存在しない session_id で呼ぶ (FK 違反、または turn_results の挿入を失敗させる) と、シートも progress も変わらない
  - `TestDeleteTurnLogsAfter`: id が afterID より大きい、そのターンの行だけが消える (他のターンの行は残る)
  - `TestRestoreBeforeTurnDropsProgress`: `RestoreBeforeTurn` の後、progress が無い
- [ ] **Step 2: 走らせて失敗を確かめる** — `go test ./pkg/db/ -run 'TurnProgress|StopRunning|MarkTurn|FinishTurn|DeleteTurnLogs|RestoreBeforeTurnDrops'` → コンパイルエラー
- [ ] **Step 3: 実装する** — 表は spec のとおり (`session_id INTEGER PRIMARY KEY REFERENCES sessions(id) ON DELETE CASCADE`)。`deleteTurnsFrom` に `DELETE FROM turn_progress WHERE session_id = ? AND turn >= ?` を足す
- [ ] **Step 4: 試験が通るのを確かめる** — `go test ./pkg/db/` → PASS
- [ ] **Step 5: コミット** — `feat(db): turn_progress for resumable turns`

### Task 2: ターンの締めを 1 トランザクションにし、turn_results の保存をエンジンへ移す

**Files:**
- Modify: `orchestrator/pkg/game/turn.go` (`applyDeltas` / `applyFormationChanges` を純粋な関数に置き換え、`finish` を足す)
- Modify: `orchestrator/pkg/api/sessions.go` (`startTurn` の `SaveTurnResult` 呼び出しを外す。`extras.go` の場面画像を付ける保存はそのまま)
- Test: `orchestrator/pkg/game/turn_test.go`

**Interfaces:**
- Consumes: `db.FinishTurn`
- Produces:
  ```go
  // nextSheets applies deltas, then lane changes, to copies of sheets.
  func nextSheets(deltas []ResourceDelta, actions []NPCAction, sheets []db.NPCSheet) ([]db.NPCSheet, []FormationChange)
  func (r *turnRun) finish(sheets []db.NPCSheet) error // tr.Deltas/FormationChanges を埋め、db.FinishTurn を呼ぶ
  ```
  `ProcessTurn` は締めまで済ませてから TurnResult を返す (ModeGM も `finish(nil)` で turn_results に保存する)。

- [ ] **Step 1: 失敗する試験を書く**
  - `TestNextSheetsLaneChangeKeepsDelta`: HP 30 の NPC が hp_delta -5・new_lane front (元 party) → HP 25 かつ PositionY 4 (Review Focus 1)
  - `TestProcessTurnSavesTurnResult`: `ProcessTurn` の後、`db.ListTurnResults` に 1 行、中身の turn が一致
- [ ] **Step 2: 走らせて失敗を確かめる** — `go test ./pkg/game/ -run 'NextSheets|SavesTurnResult'` → FAIL
- [ ] **Step 3: 実装する** — 上の Interfaces。API 側は保存を外し、結果の通知・あらすじ・場面画像だけにする
- [ ] **Step 4: 全体の試験が通るのを確かめる** — `go test ./...` → PASS (`pkg/api` の既存の試験も)
- [ ] **Step 5: コミット** — `fix(game): finish a turn in one transaction; lane changes no longer drop HP/MP deltas`

### Task 3: 段ごとの保存と `ResumeTurn`

**Files:**
- Create: `orchestrator/pkg/game/progress.go` (保存の形・保存・読み直し)
- Modify: `orchestrator/pkg/game/turn.go` (段を「終わった段は飛ばす」形に、`ResumeTurn`)
- Modify: `orchestrator/pkg/game/transcript.go` (土台より後の部分を出し入れする)
- Test: `orchestrator/pkg/game/resume_test.go`

**Interfaces:**
- Consumes: Task 1 の db 関数、Task 2 の `finish`
- Produces:
  ```go
  type turnState struct {
      Done       []string      `json:"done"`       // "gm", "npc:<名前>", "support"
      Transcript []llm.Message `json:"transcript"` // 土台より後 (record した instruction/answer の組)
      Facts      []string      `json:"facts"`
      Result     TurnResult    `json:"result"`
      NPCs       []string      `json:"npcs"`       // 開始時の NPC 名
      LogMark    int64         `json:"log_mark"`   // 最後に終わった段の時点の session_logs の最大 id
  }
  func (t *transcript) added() []llm.Message          // newTranscript に渡した土台より後
  func (t *transcript) replay(added []llm.Message)
  func (r *turnRun) saveProgress() error              // 段の結果・判定を反映した後に呼ぶ。LogMark も取り直す
  func (r *turnRun) isDone(key string) bool
  var ErrNoStoppedTurn = errors.New("続きから再開できるターンがありません")
  var ErrPartyChanged  = errors.New("止まった後にパーティが変わったため、続きから再開できません。取り消してください")
  func (e *Engine) ResumeTurn(ctx context.Context, sessionID int64, ev Events) (*TurnResult, error)
  ```
  - `ProcessTurn`: チェックポイントと player の行の後で progress を作る (status=running、Done 空)。その前の失敗は今までどおり
  - 保存の時点: GM の後 (判定を振った後)、NPC 1 体ごと (休んだ NPC も Done に入れる。行動は `r.tr.NPCActions` に 1 体ずつ足す)、Support の後
  - `ResumeTurn`: progress が無い・running → `ErrNoStoppedTurn`。NPC の名前の列が今のシートと違う → `ErrPartyChanged`。
    `DeleteTurnLogsAfter(LogMark)` → `loadContext` で土台を作る → `replay` → status=running にして、終わっていない段から続ける。チェックポイントと player の行は作らない
  - 再開した段の `StageAttempts` は、その段の新しい試行回数で上書きする

- [ ] **Step 1: 失敗する試験を書く** (`resume_test.go`。`scriptedLLM` に「n 回目の呼び出しで通信エラー」を返す形を足す。NPC は A・B・C)
  - `TestResumeAfterFailureAtEachStage`: GM / NPC A / NPC B / Support で通信エラー → `ResumeTurn` が成功。
    再開後の最初の呼び出しが止まった段の instruction で、それより前の段は呼ばない
  - `TestResumeSendsSameMessages`: 止まらずに進んだ場合の各段のリクエストと、止まって再開した場合のリクエストが同じ (`reflect.DeepEqual`)。
    `hasPrefix` の性質も再開をまたいで成り立つ
  - `TestResumeDoesNotRerollDice`: GM が判定を求め、NPC B で止まる。数える d6 (`SetDice`) の呼び出し回数と `Result.Checks` が、止まらずに進んだ場合と同じ
  - `TestResumeFromNewEngine`: 同じ DB で `NewEngine` を作り直してから再開できる
  - `TestResumeDropsStoppedStageLogs`: NPC B で `[NG]` を 1 回書いた後に通信エラー → 再開して完了。session_logs の (role, content) の列が、止まらずに進んだ場合と同じ
  - `TestResumeTwiceAfterSecondFailure`: 再開中にもう一度止まる → さらに再開して完了。全体の呼び出しは止まった段の分だけ増える (Review Focus 4)
  - `TestResumeFromGMFailure`: GM で止まる → 再開は GM から。player の行・チェックポイントは 1 つ (Review Focus 5)
  - `TestResumePartyChanged`: 止まった後に NPC を 1 体消す → `ErrPartyChanged`
  - `TestFinishedTurnHasNoProgress`: 完了後は `GetTurnProgress` が nil
- [ ] **Step 2: 走らせて失敗を確かめる** — `go test ./pkg/game/ -run Resume` → コンパイルエラー
- [ ] **Step 3: 実装する** — 上の Interfaces。新しく始めるときと再開とで、段を回す関数は 1 つにする
- [ ] **Step 4: 全体の試験が通るのを確かめる** — `go test ./...` → PASS
- [ ] **Step 5: コミット** — `feat(game): save each stage and resume a stopped turn`

### Task 4: API — 止まったターンを残す・再開・取り消し・編集の拒否

**Files:**
- Modify: `orchestrator/pkg/api/sessions.go` (`startTurn` の失敗時、`/resume`、`/discard`、`stopped_turn`)
- Modify: `orchestrator/pkg/api/handler.go` (ルート、編集の拒否)
- Modify: `orchestrator/cmd/*/main.go` (起動時に `db.StopRunningTurns`)
- Modify: `docs/api.md`
- Test: `orchestrator/pkg/api/sessions_test.go`

**Interfaces:**
- Consumes: Task 1 の db 関数、`Engine.ResumeTurn`、`ErrNoStoppedTurn`、`ErrPartyChanged`
- Produces:
  - `startTurn(sessionID, text, mode, prepare, run func(ctx, ev) (*game.TurnResult, error))`: 走らせる関数を受け取る形にし、新しいターンと再開とで共有する
  - 失敗時: その turn の progress があれば `MarkTurnStopped` して、WebSocket に `{"type":"error","turn":N,"message":…,"resumable":true}`。無ければ今までどおり `RestoreBeforeTurn`
  - `POST /api/sessions/{id}/resume` → 202 `{turn}`。止まったターンが無い → 404、`ErrPartyChanged` → 409、実行中 → 409 (`errBusy`)
  - `POST /api/sessions/{id}/discard` → 204。`RestoreBeforeTurn` (失敗したら `DeleteTurnsFrom`) と `DeleteTurnProgress`
  - `GET /api/sessions/{id}` の state に `stopped_turn`: `null` か `{turn, input, mode, error, done: []string, result: TurnResult}`
  - `func (h *Handler) refuseIfStopped(w http.ResponseWriter, sessionID int64) bool`: 止まったターンがあれば 409 `{"error":"止まったターンがあります。「続きから」か「取り消す」を先に選んでください"}` を書いて true
  - `func (h *Handler) refuseIfAnyStopped(w http.ResponseWriter) bool`: PC・クエストの編集用 (この 2 つはセッションに属さないため、どこかのセッションで止まっていれば断る)
  - 拒否する所: turn, redo (止まったターン自身は取り消してからやり直す), restore, quest, npc-position, npc-sheet の追加・削除・更新 (更新はシートの id からセッションを引く), quests の受注・完了, PC の作成以外の更新・削除・切り替え

- [ ] **Step 1: 失敗する試験を書く** (偽の LLM は既存の試験の形に合わせ、n 回目で接続を切る)
  - `TestTurnFailureKeepsProgress`: NPC で止まる → error に `resumable: true`、state の `stopped_turn.done` に `gm`、npc_sheets は開始前のまま
  - `TestResumeEndpointCompletesTurn`: `/resume` → `turn` メッセージ、`stopped_turn` が null、turns に 1 件
  - `TestDiscardEndpoint`: `/discard` → `stopped_turn` が null、turns が開始前と同じ
  - `TestEditsRefusedWhileStopped`: 止まっている間、新しいターン・npc-position・npc-sheet 更新・クエストの受注・PC の切り替えが 409
  - `TestResumeTwiceIsBusy`: `/resume` を続けて 2 回 → 2 回目は 409 (Review Focus 2)
  - `TestCancelKeepsProgress`: 実行中に `/cancel` → `resumable: true` で止まる
  - `TestStartupStopsRunningTurns`: running の progress を置いて起動処理 (`db.StopRunningTurns`) → state の `stopped_turn` が出て、`/resume` で完了 (Review Focus 3)
- [ ] **Step 2: 走らせて失敗を確かめる** — `go test ./pkg/api/ -run 'Progress|Resume|Discard|Refused|Cancel|Startup'` → FAIL
- [ ] **Step 3: 実装する** — 上の Interfaces。`turnErrorMessage` の「状態は開始前に戻っています」は、resumable のときは「途中まで残っています。続きから再開できます」に変える
- [ ] **Step 4: 全体の試験が通るのを確かめる** — `go test ./...` → PASS
- [ ] **Step 5: docs/api.md に `/resume`・`/discard`・`stopped_turn`・`resumable`・409 の一覧を書く**
- [ ] **Step 6: コミット** — `feat(api): keep a stopped turn; resume or discard it`

### Task 5: 画面 — 「続きから」「取り消す」

**Files:**
- Modify: `ui/src/types.ts` (`SessionState.stopped_turn`、error メッセージの `resumable`)
- Modify: `ui/src/live/turnReducer.ts`、`ui/src/hooks/useSessionGame.ts`
- Modify: `ui/src/views/session/SessionView.tsx`、`LiveTurnView.tsx`、`Composer.tsx`
- Modify: `ui/src/dev/mock.ts` (`/resume`・`/discard`・`stopped_turn` の偽物)

**Interfaces:**
- Consumes: Task 4 の API

- [ ] **Step 1: 実装する**
  - `stopped_turn` があるとき: 終わった段の結果 (GM の語り・行動した NPC・判定) を出したまま、「▶ 続きから」と「取り消す」を並べる。
    error の文言も出す。入力欄は送れなくする (送ると 409 になるため)
  - 「続きから」→ `POST /resume`。「取り消す」→ 確認してから `POST /discard`。どちらも後で state を読み直す
  - 「⏹ 止める」の title を「このターンを止める (続きから再開できる)」に変える
  - 再起動後に開いた画面でも、state の `stopped_turn` だけで同じ表示になる
- [ ] **Step 2: 確かめる** — `cd ui && npm run lint && npm run build` → エラーなし。`npm run dev` の mock で、止まる → 続きから → 完了、止まる → 取り消す、を画面で通す
- [ ] **Step 3: コミット** — `feat(ui): resume or discard a stopped turn`
