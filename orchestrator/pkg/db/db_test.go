package db

import (
	"database/sql"
	"testing"
)

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	d, err := Open(":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

func TestSessionCRUD(t *testing.T) {
	d := openTestDB(t)

	s, err := CreateSession(d, "quest-001")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if s.QuestID != "quest-001" {
		t.Errorf("QuestID = %q, want quest-001", s.QuestID)
	}

	got, err := GetSession(d, s.ID)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if got.ID != s.ID {
		t.Errorf("ID mismatch: got %d want %d", got.ID, s.ID)
	}
}

func TestNPCSheetUpsert(t *testing.T) {
	d := openTestDB(t)
	s, _ := CreateSession(d, "q")

	sheet := NPCSheet{SessionID: s.ID, Name: "ガルド", HP: 30, MP: 10, YAMLBlob: "hp: 30"}
	if err := UpsertNPCSheet(d, sheet); err != nil {
		t.Fatalf("UpsertNPCSheet: %v", err)
	}
	sheet.HP = 25
	if err := UpsertNPCSheet(d, sheet); err != nil {
		t.Fatalf("UpsertNPCSheet (update): %v", err)
	}
	sheets, err := GetNPCSheets(d, s.ID)
	if err != nil {
		t.Fatalf("GetNPCSheets: %v", err)
	}
	if len(sheets) != 1 {
		t.Fatalf("expected 1 sheet, got %d", len(sheets))
	}
	if sheets[0].HP != 25 {
		t.Errorf("HP = %d, want 25", sheets[0].HP)
	}
}

func TestCheckpointPrune(t *testing.T) {
	d := openTestDB(t)
	s, _ := CreateSession(d, "q")

	for i := 0; i < maxCheckpoints+3; i++ {
		if err := SaveCheckpoint(d, s.ID, i, `{}`); err != nil {
			t.Fatalf("SaveCheckpoint turn=%d: %v", i, err)
		}
	}
	cps, err := ListCheckpoints(d, s.ID)
	if err != nil {
		t.Fatalf("ListCheckpoints: %v", err)
	}
	if len(cps) != maxCheckpoints {
		t.Errorf("checkpoint count = %d, want %d", len(cps), maxCheckpoints)
	}
}

func TestRAGSearch(t *testing.T) {
	d := openTestDB(t)

	chunks := []RAGChunk{
		{SourceType: "rulebook", Priority: 0, Text: "ルールA", Enabled: true, Embedding: []float32{1, 0, 0}},
		{SourceType: "correction", Priority: 10, Text: "訂正B", Enabled: true, Embedding: []float32{1, 0, 0}},
		{SourceType: "houserule", Priority: 20, Text: "ハウスルールC", Enabled: true, Embedding: []float32{0, 1, 0}},
	}
	for _, c := range chunks {
		if _, err := InsertRAGChunk(d, c); err != nil {
			t.Fatalf("InsertRAGChunk: %v", err)
		}
	}

	// query = [1,0,0]: should return rulebook and correction (same direction),
	// correction should rank above rulebook due to priority
	results, err := SearchRAGChunks(d, []float32{1, 0, 0}, 2)
	if err != nil {
		t.Fatalf("SearchRAGChunks: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if results[0].SourceType != "correction" {
		t.Errorf("top result should be correction (priority tiebreak), got %s", results[0].SourceType)
	}
}

func TestAppendLog(t *testing.T) {
	d := openTestDB(t)
	s, _ := CreateSession(d, "q")
	if err := AppendLog(d, s.ID, 1, "player", "攻撃する"); err != nil {
		t.Fatalf("AppendLog: %v", err)
	}
}

func TestRAGSearchExcludesOverriddenChunk(t *testing.T) {
	d := openTestDB(t)

	origID, err := InsertRAGChunk(d, RAGChunk{SourceType: "rulebook", Text: "誤った記述", Enabled: true, Embedding: []float32{1, 0, 0}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := InsertRAGChunk(d, RAGChunk{SourceType: "correction", Priority: 10, Text: "正しい記述", Enabled: true,
		OverridesID: &origID, Embedding: []float32{0.9, 0.1, 0}}); err != nil {
		t.Fatal(err)
	}

	results, err := SearchRAGChunks(d, []float32{1, 0, 0}, 5)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range results {
		if r.ID == origID {
			t.Fatalf("overridden chunk %d should not be returned: %+v", origID, results)
		}
	}
	if len(results) != 1 || results[0].Text != "正しい記述" {
		t.Errorf("results = %+v", results)
	}
}

func TestRAGSearchKeepsChunkWhenCorrectionDisabled(t *testing.T) {
	d := openTestDB(t)
	origID, _ := InsertRAGChunk(d, RAGChunk{SourceType: "rulebook", Text: "元の記述", Enabled: true, Embedding: []float32{1, 0, 0}})
	_, _ = InsertRAGChunk(d, RAGChunk{SourceType: "correction", Text: "無効な訂正", Enabled: false,
		OverridesID: &origID, Embedding: []float32{1, 0, 0}})

	results, _ := SearchRAGChunks(d, []float32{1, 0, 0}, 5)
	if len(results) != 1 || results[0].ID != origID {
		t.Errorf("a disabled correction must not hide the original: %+v", results)
	}
}

func TestRAGChunkSectionAndPagesRoundTrip(t *testing.T) {
	d := openTestDB(t)
	id, err := InsertRAGChunk(d, RAGChunk{
		SourceType: "rulebook", Source: "rulebook1", Section: "第二部 ルール > 戦闘のルール > 戦闘の流れ",
		PageFrom: 128, PageTo: 129, Tag: "combat", Text: "戦闘は先制判定から始まります。", Enabled: true,
		Embedding: []float32{1, 0, 0},
	})
	if err != nil {
		t.Fatal(err)
	}

	found, _ := SearchRAGChunks(d, []float32{1, 0, 0}, 1)
	listed, _ := ListRAGChunks(d, "rulebook")
	for name, got := range map[string][]RAGChunk{"search": found, "list": listed} {
		if len(got) != 1 {
			t.Fatalf("%s: got %d chunks", name, len(got))
		}
		c := got[0]
		if c.ID != id || c.Source != "rulebook1" || c.Section != "第二部 ルール > 戦闘のルール > 戦闘の流れ" ||
			c.PageFrom != 128 || c.PageTo != 129 {
			t.Errorf("%s: %+v", name, c)
		}
	}
}

func TestOpenMigratesOldRAGTable(t *testing.T) {
	path := t.TempDir() + "/old.db"
	old, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := old.Exec(`CREATE TABLE rag_chunks (
		id INTEGER PRIMARY KEY AUTOINCREMENT, source_type TEXT NOT NULL, priority INTEGER NOT NULL,
		text TEXT NOT NULL, embedding BLOB)`); err != nil {
		t.Fatal(err)
	}
	if _, err := old.Exec(`INSERT INTO rag_chunks (source_type, priority, text) VALUES ('rulebook', 0, '旧データ')`); err != nil {
		t.Fatal(err)
	}
	old.Close()

	d, err := Open(path)
	if err != nil {
		t.Fatalf("Open old DB: %v", err)
	}
	defer d.Close()
	chunks, err := ListRAGChunks(d, "")
	if err != nil || len(chunks) != 1 || chunks[0].Text != "旧データ" || chunks[0].Section != "" {
		t.Fatalf("chunks=%+v err=%v", chunks, err)
	}
}

func TestRestoreBeforeTurnPutsSheetsBackAndDropsLaterTurns(t *testing.T) {
	d := openTestDB(t)
	s, _ := CreateSessionTitled(d, "t", "")
	_ = UpsertNPCSheet(d, NPCSheet{SessionID: s.ID, Name: "ガルド", HP: 30, MP: 10, HPMax: 30, MPMax: 10, PositionY: 4})
	sheets, _ := GetNPCSheets(d, s.ID)
	snap := `{"npc_sheets":[{"Name":"ガルド","HP":30,"MP":10,"PositionY":4}]}` // 旧形式 (Go のフィールド名) も読める
	_ = SaveCheckpoint(d, s.ID, 2, snap)
	_ = SaveTurnResult(d, s.ID, 1, `{"turn":1}`)
	_ = SaveTurnResult(d, s.ID, 2, `{"turn":2}`)
	_ = SaveTurnResult(d, s.ID, 3, `{"turn":3}`)
	_ = AppendLog(d, s.ID, 2, "gm", "x")
	_ = AddSummary(d, s.ID, 2, []string{"要約"})

	sheets[0].HP, sheets[0].MP, sheets[0].PositionY = 5, 0, 7
	_ = UpsertNPCSheet(d, sheets[0])
	_ = UpsertNPCSheet(d, NPCSheet{SessionID: s.ID, Name: "後から追加", HP: 1})

	if err := RestoreBeforeTurn(d, s.ID, 2); err != nil {
		t.Fatalf("RestoreBeforeTurn: %v", err)
	}
	got, _ := GetNPCSheets(d, s.ID)
	if len(got) != 1 || got[0].HP != 30 || got[0].MP != 10 || got[0].PositionY != 4 {
		t.Errorf("sheets = %+v", got)
	}
	if n, _ := LatestTurn(d, s.ID); n != 1 {
		t.Errorf("LatestTurn = %d, want 1", n)
	}
	if sum, _ := ListSummary(d, s.ID); len(sum) != 0 {
		t.Errorf("summary covering turn 2 should be gone: %v", sum)
	}
	if _, err := GetCheckpoint(d, s.ID, 2); err == nil {
		t.Error("checkpoint of turn 2 should be gone")
	}
}

func TestSessionsListAndSummary(t *testing.T) {
	d := openTestDB(t)
	a, _ := CreateSessionTitled(d, "古い", "")
	b, _ := CreateSessionTitled(d, "新しい", "3")
	_, _ = d.Exec(`UPDATE sessions SET updated_at = '2000-01-01' WHERE id = ?`, a.ID)
	list, err := ListSessions(d)
	if err != nil || len(list) != 2 || list[0].ID != b.ID || list[0].QuestID != "3" {
		t.Fatalf("list=%+v err=%v", list, err)
	}
	_ = AddSummary(d, b.ID, 3, []string{"一行目", "二行目"})
	_ = AddSummary(d, b.ID, 6, []string{"三行目"})
	sum, _ := ListSummary(d, b.ID)
	upto, _ := SummarizedUpto(d, b.ID)
	if len(sum) != 3 || sum[2] != "三行目" || upto != 6 {
		t.Errorf("sum=%v upto=%d", sum, upto)
	}
}

func TestRAGSearchPriorityBeatsSlightlyCloserRulebook(t *testing.T) {
	d := openTestDB(t)
	_, _ = InsertRAGChunk(d, RAGChunk{SourceType: "rulebook", Text: "本文", Enabled: true, Embedding: []float32{1, 0.05, 0}})
	_, _ = InsertRAGChunk(d, RAGChunk{SourceType: "houserule", Priority: 20, Text: "ハウスルール", Enabled: true, Embedding: []float32{1, 0.2, 0}})
	got, _ := SearchRAGChunks(d, []float32{1, 0, 0}, 2)
	if got[0].SourceType != "houserule" || got[0].Score <= 0 {
		t.Errorf("got %+v", got)
	}
}
