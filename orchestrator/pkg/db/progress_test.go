package db

import (
	"database/sql"
	"path/filepath"
	"testing"
)

func countRows(t *testing.T, d *sql.DB, q string, args ...any) int {
	t.Helper()
	var n int
	if err := d.QueryRow(q, args...).Scan(&n); err != nil {
		t.Fatalf("count %q: %v", q, err)
	}
	return n
}

func TestTurnProgressRoundTrip(t *testing.T) {
	d := openTestDB(t)
	s, _ := CreateSessionTitled(d, "t", "")
	p := TurnProgress{SessionID: s.ID, Turn: 3, Input: "扉を開ける", Mode: "play", StateJSON: `{"a":1}`, Status: ProgressRunning}
	if err := SaveTurnProgress(d, p); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := GetTurnProgress(d, s.ID)
	if err != nil || got == nil {
		t.Fatalf("Get: %v %v", got, err)
	}
	if got.Turn != 3 || got.Input != "扉を開ける" || got.Mode != "play" || got.StateJSON != `{"a":1}` || got.Status != ProgressRunning || got.Error != "" || got.UpdatedAt.IsZero() {
		t.Errorf("got %+v", got)
	}
	p.Turn, p.StateJSON, p.Status, p.Error = 4, `{"b":2}`, ProgressStopped, "boom"
	if err := SaveTurnProgress(d, p); err != nil {
		t.Fatalf("Save 2: %v", err)
	}
	got, _ = GetTurnProgress(d, s.ID)
	if got.Turn != 4 || got.StateJSON != `{"b":2}` || got.Status != ProgressStopped || got.Error != "boom" {
		t.Errorf("overwrite: %+v", got)
	}
	if n := countRows(t, d, `SELECT COUNT(*) FROM turn_progress WHERE session_id = ?`, s.ID); n != 1 {
		t.Errorf("rows = %d, want 1", n)
	}
}

func TestGetTurnProgressNone(t *testing.T) {
	d := openTestDB(t)
	got, err := GetTurnProgress(d, 999)
	if got != nil || err != nil {
		t.Errorf("got %v, %v; want nil, nil", got, err)
	}
}

func TestStopRunningTurns(t *testing.T) {
	d := openTestDB(t)
	st := []string{ProgressRunning, ProgressRunning, ProgressStopped}
	for i, status := range st {
		s, _ := CreateSessionTitled(d, "t", "")
		if err := SaveTurnProgress(d, TurnProgress{SessionID: s.ID, Turn: i + 1, Status: status}); err != nil {
			t.Fatal(err)
		}
	}
	n, err := StopRunningTurns(d)
	if err != nil || n != 2 {
		t.Fatalf("StopRunningTurns = %d, %v; want 2", n, err)
	}
	if c := countRows(t, d, `SELECT COUNT(*) FROM turn_progress WHERE status = 'stopped'`); c != 3 {
		t.Errorf("stopped = %d, want 3", c)
	}
	if c := countRows(t, d, `SELECT COUNT(*) FROM turn_progress WHERE error = 'オーケストレーターの再起動で止まりました'`); c != 2 {
		t.Errorf("rows with the restart reason = %d, want 2", c)
	}
}

func TestFirstStoppedSession(t *testing.T) {
	d := openTestDB(t)
	s, _ := CreateSessionTitled(d, "森の調査", "")
	if st, err := FirstStoppedSession(d); err != nil || st != nil {
		t.Errorf("empty: %v %v", st, err)
	}
	_ = SaveTurnProgress(d, TurnProgress{SessionID: s.ID, Turn: 1, Status: ProgressRunning})
	if st, _ := FirstStoppedSession(d); st != nil {
		t.Error("running only should be nil")
	}
	if ids, _ := StoppedSessionIDs(d); len(ids) != 0 {
		t.Errorf("running only: ids = %v", ids)
	}
	_ = MarkTurnStopped(d, s.ID, "x")
	st, err := FirstStoppedSession(d)
	if err != nil || st == nil || st.ID != s.ID || st.Title != "森の調査" {
		t.Errorf("stopped: %v %v", st, err)
	}
	if ids, _ := StoppedSessionIDs(d); !ids[s.ID] || len(ids) != 1 {
		t.Errorf("ids = %v", ids)
	}
}

func TestMarkTurnStopped(t *testing.T) {
	d := openTestDB(t)
	s, _ := CreateSessionTitled(d, "t", "")
	_ = SaveTurnProgress(d, TurnProgress{SessionID: s.ID, Turn: 1, Status: ProgressRunning})
	if err := MarkTurnStopped(d, s.ID, "LLM timeout"); err != nil {
		t.Fatal(err)
	}
	got, _ := GetTurnProgress(d, s.ID)
	if got.Status != ProgressStopped || got.Error != "LLM timeout" {
		t.Errorf("got %+v", got)
	}
}

func TestFinishTurnAtomic(t *testing.T) {
	// foreign_keys is a pragma in the file DSN only, so use a file DB.
	d, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	s, _ := CreateSessionTitled(d, "t", "")
	_ = UpsertNPCSheet(d, NPCSheet{SessionID: s.ID, Name: "ガルド", HP: 30, HPMax: 30})
	_ = SaveTurnProgress(d, TurnProgress{SessionID: s.ID, Turn: 1, Status: ProgressRunning})

	// Success path.
	if err := FinishTurn(d, s.ID, 1, []NPCSheet{{SessionID: s.ID, Name: "ガルド", HP: 12, HPMax: 30}}, `{"turn":1}`); err != nil {
		t.Fatalf("FinishTurn: %v", err)
	}
	sh, _ := GetNPCSheets(d, s.ID)
	if len(sh) != 1 || sh[0].HP != 12 {
		t.Errorf("sheets = %+v", sh)
	}
	if n := countRows(t, d, `SELECT COUNT(*) FROM turn_results WHERE session_id = ? AND turn = 1`, s.ID); n != 1 {
		t.Errorf("turn_results rows = %d", n)
	}
	if p, _ := GetTurnProgress(d, s.ID); p != nil {
		t.Errorf("progress should be gone: %+v", p)
	}

	// Failure path: sheet upsert succeeds, then turn_results insert violates
	// the FK (no such session) -> everything must roll back.
	_ = SaveTurnProgress(d, TurnProgress{SessionID: s.ID, Turn: 2, Status: ProgressRunning})
	err = FinishTurn(d, 9999, 2, []NPCSheet{{SessionID: s.ID, Name: "ガルド", HP: 1, HPMax: 30}}, `{"turn":2}`)
	if err == nil {
		t.Fatal("expected FK failure")
	}
	sh, _ = GetNPCSheets(d, s.ID)
	if len(sh) != 1 || sh[0].HP != 12 {
		t.Errorf("sheets changed after failed finish: %+v", sh)
	}
	if p, _ := GetTurnProgress(d, s.ID); p == nil || p.Turn != 2 {
		t.Errorf("progress changed after failed finish: %+v", p)
	}
	if n := countRows(t, d, `SELECT COUNT(*) FROM turn_results`); n != 1 {
		t.Errorf("turn_results rows = %d, want 1", n)
	}
}

func TestDeleteTurnLogsAfter(t *testing.T) {
	d := openTestDB(t)
	s, _ := CreateSessionTitled(d, "t", "")
	_ = AppendLog(d, s.ID, 1, "gm", "t1")
	_ = AppendLog(d, s.ID, 2, "player", "a")
	after, err := MaxLogID(d, s.ID)
	if err != nil || after == 0 {
		t.Fatalf("MaxLogID = %d, %v", after, err)
	}
	_ = AppendLog(d, s.ID, 2, "gm", "b")
	_ = AppendLog(d, s.ID, 3, "gm", "other turn")
	if err := DeleteTurnLogsAfter(d, s.ID, 2, after); err != nil {
		t.Fatal(err)
	}
	if n := countRows(t, d, `SELECT COUNT(*) FROM session_logs WHERE session_id = ?`, s.ID); n != 3 {
		t.Errorf("logs = %d, want 3", n)
	}
	if n := countRows(t, d, `SELECT COUNT(*) FROM session_logs WHERE content = 'b'`); n != 0 {
		t.Error("log b should be deleted")
	}
	empty, _ := CreateSessionTitled(d, "e", "")
	if id, err := MaxLogID(d, empty.ID); id != 0 || err != nil {
		t.Errorf("MaxLogID empty = %d, %v", id, err)
	}
}

func TestRestoreBeforeTurnDropsProgress(t *testing.T) {
	d := openTestDB(t)
	s, _ := CreateSessionTitled(d, "t", "")
	_ = SaveCheckpoint(d, s.ID, 2, `{"npc_sheets":[]}`)
	_ = SaveTurnProgress(d, TurnProgress{SessionID: s.ID, Turn: 2, Status: ProgressStopped})
	if err := RestoreBeforeTurn(d, s.ID, 2); err != nil {
		t.Fatal(err)
	}
	if p, _ := GetTurnProgress(d, s.ID); p != nil {
		t.Errorf("progress remains: %+v", p)
	}
}
