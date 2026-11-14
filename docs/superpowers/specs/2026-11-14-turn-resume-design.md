# ターンの途中からの再開 (設計)

2026-11-14

## 目的

ターンが LLM の通信失敗・中断ボタン・時間切れ (TURN_TIMEOUT_SEC=900) で止まると、今は `RestoreBeforeTurn` で開始前に戻り、
それまでに終わった段 (GM の語り 約 40 秒・NPC の行動・振ったサイコロ) をすべて捨てている。
終わった段を段ごとに DB に残し、止まった段から続けられるようにする。

mlx-serve が落ちた・GPU が急に遅くなった後は、オーケストレーターも起動し直すことが多い。
再起動しても消えないように、置き場は DB だけにする (メモリに持つ案 A は不採用。下の「案」)。

## 目標

- 止まったターンを、終わった段を作り直さずに続けられる (「続きから」)
- 続けずに捨てることもできる (「取り消す」= 今と同じく開始前に戻す)
- オーケストレーターを再起動しても続けられる
- 続けたターンの結果は、止まらずに進んだ場合と同じ形 (TurnResult・session_logs・turn_results) になる

## 目標でないこと

- 自動の再試行 (案 C)。B の上に後から足せる
- 段の途中 (流している最中の文) からの再開。止まった段は最初から作り直す
- 1 セッションに複数の止まったターンを持つこと
- mlx-serve (Python) 側のチャット・即興への適用

## してはいけないこと

- **振ったサイコロを振り直すこと.** 判定結果 (facts・Checks) は段と一緒に残し、再開ではそのまま使う。振り直せると結果を選べてしまう
- **APC の prefix を崩すこと.** 再開後の段のリクエストは、止まらずに進んだ場合と同じメッセージ列にする
  (transcript を同じ順に積み直す。`stage_test.go` の prefix の性質が再開をまたいでも成り立つ)
- 止まったターンの途中結果が、続きも取り消しもされないまま次のターンに混ざること

## 制約 (今の作り)

- 1 ターン = `ProcessTurn` (`pkg/game/turn.go`): GM → NPC を 1 体ずつ → Support → `applyDeltas` / 隊列の変更。
  ModeGM (GM への訴え) は GM だけ
- NPC シートが書き換わるのは最後の `applyDeltas` / `applyFormationChanges` だけ。途中で止まってもシートは開始前のまま
- 検証を通らない NPC はそのターンを休む (`Warnings`)。Support は NPC 自身の値で代わりに進む。
  ターンが止まるのは、通信の失敗・中断・時間切れ・**GM 段の再試行が尽きたとき**・DB のエラー
- 段が通ると `session_logs` に書かれる (`runStage`)。検証 NG の試行も `[NG n/m]` の行として書かれる。
  失敗時は `RestoreBeforeTurn` がターンの行を消す
- `loadContext` (`pkg/game/context.go`) の土台は turn_results だけでなく、受注クエスト・有効な PC・NPC シート (人物設定)・あらすじからも作る
- `SaveTurnResult` を呼ぶのは API 側 (`startTurn` のゴルーチン)。`applyDeltas` / `applyFormationChanges` は NPC を 1 体ずつ書き、トランザクションではない
- ターンは `startTurn` (`pkg/api/sessions.go`) のゴルーチンで動き、セッションごとに 1 本だけ (`errBusy`)

## 案

| 案 | 中身 | 判断 |
|---|---|---|
| A. メモリに持つ | 失敗したターンの途中結果をオーケストレーターのメモリに置き、「続きから」で使う | 不採用. 再起動で消える。DB から読むのとの差は数ミリ秒で、すぐ再開する速さは mlx-vlm の APC が受け持つ |
| **B. DB に段ごとに残す** | 段が終わるたびに途中結果を DB に保存. 失敗しても開始前に戻さない | **採用** |
| C. B + 自動の再試行 | 通信の失敗なら `/health` が戻るのを待って自動で続ける | 今回はしない. GPU の急な遅さで時間切れを繰り返すおそれ |

利用者の判断 (2026-11-14): B を主体、読み出しは DB だけ (メモリ優先の経路は足さない)。

## 設計

### 保存するもの

新しい表 `turn_progress` (セッションごとに高々 1 行):

```sql
CREATE TABLE IF NOT EXISTS turn_progress (
    session_id  INTEGER PRIMARY KEY REFERENCES sessions(id) ON DELETE CASCADE,
    turn        INTEGER NOT NULL,
    input       TEXT    NOT NULL,
    mode        TEXT    NOT NULL,
    state_json  TEXT    NOT NULL, -- 下の progress
    status      TEXT    NOT NULL, -- 'running' | 'stopped'
    error       TEXT    NOT NULL DEFAULT '',
    updated_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
```

`state_json` は段が終わるたびに丸ごと書き直す (UPSERT 1 回)。中身:

- `done`: 終わった段の key の列 (`gm`, `npc:<名前>`, `support`)。休んだ NPC も「終わった」に入れる
- `transcript`: 記録された instruction と answer の組 (`transcript.record` に渡したもの) を順に
- `facts`: 判定結果の行 (`turnRun.facts`)
- `result`: ここまでの `TurnResult` (GMNarration・NPCActions・Checks・RulesUsed・Warnings・StageAttempts)
- `npcs`: ターン開始時の NPC の名前の列 (再開時の照合用)

- `log_mark`: 最後に終わった段の時点での session_logs の最大 id (下の「止まった段のログ」)

`transcript` の土台 (セッションの文脈 `loadContext`) は保存しない。再開時に作り直す。
止まったターンは turn_results に入らず、止まっている間はセッションの編集を断る (下の API) ので、作り直しても同じ内容になる。

### エンジン (`pkg/game`)

- `ProcessTurn` の始めに `turn_progress` を作る (status=running)。段が終わるたび (GM・各 NPC・Support の後) に保存
- 段を回す部分を「`done` に入っている段は飛ばす」形にし、新しく始めるときと再開とで同じ道を通す
- 新しい入口 `ResumeTurn(ctx, sessionID, ev)`: `turn_progress` を読み、`turnRun` (transcript・facts・TurnResult) を組み直し、
  残りの段から続ける。チェックポイントの保存と player の行の追加はしない (始めたときに済んでいる)
- **完了処理は 1 つのトランザクション**: deltas の適用・隊列の変更・turn_results の保存・turn_progress の削除をまとめる。
  途中で落ちても deltas が二重に当たらないように。そのため turn_results の保存は API 側からエンジン側に移す
  (`ProcessTurn` / `ResumeTurn` が保存まで済ませて結果を返す。API は通知・あらすじ・場面画像だけ)
- **止まった段のログ**: 再開の前に、そのターンの session_logs のうち id が `log_mark` より大きい行 (止まった段の `[NG]` の試行など) を消す。
  これで完了後の session_logs が、止まらずに進んだ場合と同じになる
- 再開の対象: `turn_progress` を作った後の失敗はすべて (通信・中断・時間切れ・GM の再試行切れ・DB のエラー)。
  GM の再試行切れは、再開すると GM 段からやり直す。`turn_progress` を作る前の失敗 (チェックポイントの保存など) は今と同じく開始前に戻す

### API (`pkg/api`)

- 失敗・中断・時間切れのとき: `RestoreBeforeTurn` は呼ばない。`turn_progress` を status=stopped・error 付きにし、
  `{"type":"error", ..., "resumable": true}` を送る
- `POST /api/sessions/{id}/resume`: `ResumeTurn` を `startTurn` と同じ予約 (busy・タイムアウトは新しく 900 秒) で走らせる
- `POST /api/sessions/{id}/discard`: 今の失敗時と同じく `RestoreBeforeTurn` (無ければ `DeleteTurnsFrom`) を呼び、`turn_progress` を消す
- セッションの状態 (`GET` のレスポンス) に `stopped_turn` (turn・input・終わった段・error) を足す。再起動後の画面がこれで「続きから」を出す
- **止まったターンがある間は、セッションを変える操作を 409 で断る** (「続きから」か「取り消す」を先に):
  新しいターンの送信、それより前のターンの戻す・直す、NPC シートの編集・位置・追加・削除、PC の編集・切り替え、クエストの受注・完了。
  止まったターン自身の「✎ 直す」(redo) は、取り消してから新しい入力でやり直す
- 念のため、再開時に NPC の顔ぶれが `npcs` と違うときは再開を断り、取り消しだけにする
- オーケストレーターの起動時、status=running の行は stopped にする (ゴルーチンが死んでいるので)

### 画面 (`ui/`)

- 止まったターンの所に、終わった段の結果 (GM の語り・行動した NPC) を出したまま、「続きから」「取り消す」を並べる
- 「⏹ 止める」の説明を「このターンを止める (続きから再開できる)」に変える

### 試験

`pkg/game` の Go の試験で、偽の ChatModel を使う:

- n 段目で通信エラー → `ResumeTurn` は n 段目から呼び、前の段は呼ばない
- 再開の前後で d6 が振り直されない (サイコロの呼び出し回数と出目が、止まらずに進んだ場合と同じ)
- 再開後の各段のリクエストが、止まらずに進んだ場合と同じメッセージ列 (`hasPrefix` の性質も成り立つ)
- 新しい `Engine` (同じ DB) から再開できる (再起動の代わり)
- 完了後に `turn_progress` が消え、turn_results・session_logs が止まらずに進んだ場合と同じ
- 取り消し後は今の失敗時と同じ状態

- 完了処理の途中 (適用の後) で失敗させても、deltas が二重に当たらない
- 止まった段で `[NG]` が書かれた後に再開しても、完了後の session_logs が止まらずに進んだ場合と同じ

`pkg/api` の試験: 止まったターンがある間の、セッションを変える操作の拒否 (上の一覧)、顔ぶれが変わったときの再開の拒否、起動時の running → stopped。

## 設計者の判断 (利用者が確かめるもの)

- 止まっている間はセッションの編集を断る (保存して使う・変化を検出して取り消しだけにする、の案もあった。断るのが一番単純で、APC の prefix と結果の形の保証が崩れない)
- 完了処理を 1 トランザクションにし、turn_results の保存をエンジンに移す
- 止まった段の `[NG]` のログは再開前に消す
- 中断ボタンで止めたときも途中結果を残す (今は「無かったことになる」)

## 作る人に任せること

- `state_json` の Go の型名と、保存の関数をどのパッケージに置くか (`pkg/db` に置く想定)
- `stopped_turn` で画面に返す項目の細かい形
- 画面のボタンの見た目と文言の細部
- docs/api.md・spec の更新の書き方
