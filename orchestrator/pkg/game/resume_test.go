package game

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/kf/sw25-omni-master/pkg/db"
	"github.com/kf/sw25-omni-master/pkg/llm"
)

// ── 止まったターンの再開 ─────────────────────────────────────────────────────
//
// NPC は A・B・C。止まらずに進むと呼び出しは GM, A, B, C, Support の 5 回。

const supportABC = `[{"npc_name":"A","hp_delta":-5,"mp_cost":3},{"npc_name":"B","hp_delta":-5,"mp_cost":3},{"npc_name":"C","hp_delta":-5,"mp_cost":3}]`

func abcReplies() []llm.Result {
	return []llm.Result{
		{Text: "GMの描写。", FinishReason: "stop"},
		{Text: npcYAML("A"), FinishReason: "stop"},
		{Text: npcYAML("B"), FinishReason: "stop"},
		{Text: npcYAML("C"), FinishReason: "stop"},
		{Text: supportABC, FinishReason: "stop"},
	}
}

func turnInput(sid int64) PlayerInput { return PlayerInput{SessionID: sid, Turn: 1, Text: "進む"} }

// uninterrupted runs the turn without stopping, on its own engine and DB.
func uninterrupted(t *testing.T, replies []llm.Result, dice func() int) (*Engine, *scriptedLLM, int64, *TurnResult) {
	t.Helper()
	e, m, sid := setupSerialEngine(t, replies, "A", "B", "C")
	if dice != nil {
		e.SetDice(dice)
	}
	res, err := e.ProcessTurn(context.Background(), turnInput(sid), nil)
	if err != nil {
		t.Fatalf("uninterrupted ProcessTurn: %v", err)
	}
	return e, m, sid, res
}

// stopped runs the turn with transport errors at the given calls; the turn
// must stop.
func stopped(t *testing.T, replies []llm.Result, dice func() int, failAt ...int) (*Engine, *scriptedLLM, int64) {
	t.Helper()
	e, m, sid := setupSerialEngine(t, replies, "A", "B", "C")
	if dice != nil {
		e.SetDice(dice)
	}
	m.failAt = map[int]bool{}
	for _, n := range failAt {
		m.failAt[n] = true
	}
	if _, err := e.ProcessTurn(context.Background(), turnInput(sid), nil); err == nil {
		t.Fatal("ProcessTurn should stop at the transport error")
	}
	return e, m, sid
}

// withoutFailed drops the calls that returned a transport error.
func withoutFailed(m *scriptedLLM) []scriptedCall {
	var out []scriptedCall
	for i, c := range m.calls {
		if !m.failAt[i+1] {
			out = append(out, c)
		}
	}
	return out
}

// countingDice returns 1, 2, …, 6, 1, … and counts its calls, so a re-roll
// would change both the count and the faces.
func countingDice(n *int) func() int {
	return func() int { *n++; return (*n-1)%6 + 1 }
}

type logRow struct{ role, content string }

func turnLogs(t *testing.T, e *Engine, sid int64) []logRow {
	t.Helper()
	rows, err := e.db.Query(`SELECT role, content FROM session_logs WHERE session_id = ? ORDER BY id`, sid)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []logRow
	for rows.Next() {
		var r logRow
		if err := rows.Scan(&r.role, &r.content); err != nil {
			t.Fatal(err)
		}
		out = append(out, r)
	}
	return out
}

func sameOutcome(t *testing.T, got, want *TurnResult) {
	t.Helper()
	if !reflect.DeepEqual(got.NPCActions, want.NPCActions) || !reflect.DeepEqual(got.Deltas, want.Deltas) ||
		!reflect.DeepEqual(got.Checks, want.Checks) || got.GMNarration != want.GMNarration ||
		!reflect.DeepEqual(got.Warnings, want.Warnings) || !reflect.DeepEqual(got.FormationChanges, want.FormationChanges) {
		t.Errorf("resumed result differs:\ngot  %+v\nwant %+v", got, want)
	}
}

func TestResumeAfterFailureAtEachStage(t *testing.T) {
	for _, tc := range []struct {
		name   string
		failAt int
		marker string
	}{
		{"GM", 1, "【指示: GM】"},
		{"NPC A", 2, "【指示: NPC「A」】"},
		{"NPC B", 3, "【指示: NPC「B」】"},
		{"Support", 5, "【指示: 数値計算担当】"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, _, _, want := uninterrupted(t, abcReplies(), nil)
			e, m, sid := stopped(t, abcReplies(), nil, tc.failAt)
			before := len(m.calls)

			res, err := e.ResumeTurn(context.Background(), sid, nil)
			if err != nil {
				t.Fatalf("ResumeTurn: %v", err)
			}
			first := lastContent(m.calls[before].msgs)
			if !strings.Contains(first, tc.marker) {
				t.Errorf("first call after resume should be the stopped stage %s: %q", tc.marker, first)
			}
			if first != lastContent(m.calls[tc.failAt-1].msgs) {
				t.Error("the resumed stage should get the same instruction as the stopped one")
			}
			if got := len(m.calls) - before; got != 5-tc.failAt+1 {
				t.Errorf("resume made %d calls, want %d (earlier stages must not run again)", got, 5-tc.failAt+1)
			}
			sameOutcome(t, res, want)
		})
	}
}

func TestResumeSendsSameMessages(t *testing.T) {
	for _, failAt := range []int{1, 2, 3, 4, 5} {
		_, u, _, _ := uninterrupted(t, abcReplies(), nil)
		e, m, sid := stopped(t, abcReplies(), nil, failAt)
		if _, err := e.ResumeTurn(context.Background(), sid, nil); err != nil {
			t.Fatalf("failAt=%d ResumeTurn: %v", failAt, err)
		}
		got := withoutFailed(m)
		if len(got) != len(u.calls) {
			t.Fatalf("failAt=%d: %d calls, want %d", failAt, len(got), len(u.calls))
		}
		for i := range got {
			if !reflect.DeepEqual(got[i].msgs, u.calls[i].msgs) {
				t.Errorf("failAt=%d call %d differs from the uninterrupted turn:\ngot  %+v\nwant %+v",
					failAt, i, got[i].msgs, u.calls[i].msgs)
			}
		}
		// The failed request is the same request the resumed stage sends.
		if !reflect.DeepEqual(m.calls[failAt-1].msgs, u.calls[failAt-1].msgs) {
			t.Errorf("failAt=%d: the stopped request differs from the uninterrupted one", failAt)
		}
		for i := 1; i < len(m.calls); i++ {
			prev, cur := m.calls[i-1].msgs, m.calls[i].msgs
			if !hasPrefix(cur, prev[:len(prev)-1]) {
				t.Errorf("failAt=%d: call %d does not extend call %d across the resume", failAt, i, i-1)
			}
		}
	}
}

func TestResumeDoesNotRerollDice(t *testing.T) {
	replies := abcReplies()
	replies[0] = llm.Result{Text: "ゴブリンが飛びかかる。\n判定: ギル/回避力判定/基準値6/目標値11", FinishReason: "stop"}
	replies[1] = llm.Result{Text: npcYAML("A") + "\ncheck: 命中力判定 基準値5 目標値10", FinishReason: "stop"}

	var nu, ns int
	_, _, _, want := uninterrupted(t, replies, countingDice(&nu))
	e, _, sid := stopped(t, replies, countingDice(&ns), 3)
	res, err := e.ResumeTurn(context.Background(), sid, nil)
	if err != nil {
		t.Fatalf("ResumeTurn: %v", err)
	}
	if ns != nu || nu != 4 {
		t.Errorf("d6 called %d times with a resume, %d without; want both 4", ns, nu)
	}
	if !reflect.DeepEqual(res.Checks, want.Checks) || len(res.Checks) != 2 {
		t.Errorf("Checks = %+v, want %+v", res.Checks, want.Checks)
	}
}

func TestResumeFromNewEngine(t *testing.T) {
	_, _, _, want := uninterrupted(t, abcReplies(), nil)
	e, _, sid := stopped(t, abcReplies(), nil, 3)

	// A restarted orchestrator: a new engine and model on the same DB.
	m2 := &scriptedLLM{replies: abcReplies()[2:]}
	e2 := NewEngine(e.db, m2, nil, 2)
	res, err := e2.ResumeTurn(context.Background(), sid, nil)
	if err != nil {
		t.Fatalf("ResumeTurn on a new engine: %v", err)
	}
	if len(m2.calls) != 3 || !strings.Contains(lastContent(m2.calls[0].msgs), "【指示: NPC「B」】") {
		t.Errorf("new engine made %d calls, first %q", len(m2.calls), lastContent(m2.calls[0].msgs))
	}
	sameOutcome(t, res, want)
}

func TestResumeDropsStoppedStageLogs(t *testing.T) {
	bad := llm.Result{Text: "name: B\naction: 攻撃\ndialogue: x\nmp_cost: 99", FinishReason: "stop"}
	r := abcReplies()
	plain := []llm.Result{r[0], r[1], bad, r[2], r[3], r[4]}
	// Stopped: B's first answer is NG, then the connection fails; on resume B
	// starts over (NG, then OK).
	interrupted := []llm.Result{r[0], r[1], bad, bad, r[2], r[3], r[4]}

	ue, _, usid, want := uninterrupted(t, plain, nil)
	e, _, sid := stopped(t, interrupted, nil, 4)
	res, err := e.ResumeTurn(context.Background(), sid, nil)
	if err != nil {
		t.Fatalf("ResumeTurn: %v", err)
	}
	if got, w := turnLogs(t, e, sid), turnLogs(t, ue, usid); !reflect.DeepEqual(got, w) {
		t.Errorf("session_logs differ:\ngot  %+v\nwant %+v", got, w)
	}
	if res.StageAttempts["npc:B"] != 2 || want.StageAttempts["npc:B"] != 2 {
		t.Errorf("npc:B attempts = %d (resumed), %d (uninterrupted); want 2", res.StageAttempts["npc:B"], want.StageAttempts["npc:B"])
	}
}

func TestResumeTwiceAfterSecondFailure(t *testing.T) {
	_, _, _, want := uninterrupted(t, abcReplies(), nil)
	// Call 3 (B) fails; on resume call 4 is B and call 5 (C) fails again.
	e, m, sid := stopped(t, abcReplies(), nil, 3, 5)
	if _, err := e.ResumeTurn(context.Background(), sid, nil); err == nil {
		t.Fatal("the first resume should stop at C")
	}
	p, _ := db.GetTurnProgress(e.db, sid)
	if p == nil || p.Status != db.ProgressStopped || p.Error == "" {
		t.Fatalf("progress after the second stop = %+v, want stopped with an error", p)
	}
	res, err := e.ResumeTurn(context.Background(), sid, nil)
	if err != nil {
		t.Fatalf("second ResumeTurn: %v", err)
	}
	if len(m.calls) != 7 {
		t.Errorf("calls = %d, want 7 (5 plus one per stop)", len(m.calls))
	}
	if !strings.Contains(lastContent(m.calls[5].msgs), "【指示: NPC「C」】") {
		t.Error("the second resume should start at C")
	}
	sameOutcome(t, res, want)
}

func TestResumeFromGMFailure(t *testing.T) {
	e, m, sid := stopped(t, abcReplies(), nil, 1)
	if _, err := e.ResumeTurn(context.Background(), sid, nil); err != nil {
		t.Fatalf("ResumeTurn: %v", err)
	}
	if !strings.Contains(lastContent(m.calls[1].msgs), "【指示: GM】") {
		t.Error("resume should start at the GM")
	}
	var players int
	for _, r := range turnLogs(t, e, sid) {
		if r.role == "player" {
			players++
		}
	}
	if players != 1 {
		t.Errorf("player log rows = %d, want 1", players)
	}
	cps, err := db.ListCheckpoints(e.db, sid)
	if err != nil || len(cps) != 1 {
		t.Errorf("checkpoints = %d (err %v), want 1", len(cps), err)
	}
}

func TestResumePartyChanged(t *testing.T) {
	e, _, sid := stopped(t, abcReplies(), nil, 3)
	if err := db.DeleteNPCSheet(e.db, sid, "C"); err != nil {
		t.Fatal(err)
	}
	if _, err := e.ResumeTurn(context.Background(), sid, nil); !errors.Is(err, ErrPartyChanged) {
		t.Fatalf("err = %v, want ErrPartyChanged", err)
	}
	if p, _ := db.GetTurnProgress(e.db, sid); p == nil || p.Status != db.ProgressStopped {
		t.Errorf("progress = %+v, want it kept stopped for a discard", p)
	}
}

func TestResumeNeedsStoppedTurn(t *testing.T) {
	e, _, sid := setupSerialEngine(t, abcReplies(), "A", "B", "C")
	if _, err := e.ResumeTurn(context.Background(), sid, nil); !errors.Is(err, ErrNoStoppedTurn) {
		t.Errorf("no progress: err = %v, want ErrNoStoppedTurn", err)
	}
	if err := db.SaveTurnProgress(e.db, db.TurnProgress{SessionID: sid, Turn: 1, Input: "x", Mode: ModeAction,
		StateJSON: "{}", Status: db.ProgressRunning}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.ResumeTurn(context.Background(), sid, nil); !errors.Is(err, ErrNoStoppedTurn) {
		t.Errorf("running: err = %v, want ErrNoStoppedTurn", err)
	}
}

func TestStoppedTurnKeepsProgress(t *testing.T) {
	e, _, sid := stopped(t, abcReplies(), nil, 3)
	p, err := db.GetTurnProgress(e.db, sid)
	if err != nil || p == nil {
		t.Fatalf("progress = %v, err %v", p, err)
	}
	if p.Status != db.ProgressStopped || !strings.Contains(p.Error, "connection refused") {
		t.Errorf("progress = %+v, want stopped with the transport error", p)
	}
	if p.Turn != 1 || p.Input != "進む" || p.Mode != ModeAction {
		t.Errorf("progress = %+v", p)
	}
	for _, want := range []string{`"gm"`, `"npc:A"`} {
		if !strings.Contains(p.StateJSON, want) {
			t.Errorf("state %s lacks done stage %s", p.StateJSON, want)
		}
	}
	if strings.Contains(p.StateJSON, `"npc:B"`) {
		t.Errorf("state %s marks the stopped stage done", p.StateJSON)
	}
}

func TestFinishedTurnHasNoProgress(t *testing.T) {
	e, _, sid, _ := uninterrupted(t, abcReplies(), nil)
	if p, err := db.GetTurnProgress(e.db, sid); p != nil || err != nil {
		t.Errorf("progress after a finished turn = %+v, err %v", p, err)
	}
	e2, _, sid2 := stopped(t, abcReplies(), nil, 3)
	if _, err := e2.ResumeTurn(context.Background(), sid2, nil); err != nil {
		t.Fatal(err)
	}
	if p, err := db.GetTurnProgress(e2.db, sid2); p != nil || err != nil {
		t.Errorf("progress after a resumed turn = %+v, err %v", p, err)
	}
}
