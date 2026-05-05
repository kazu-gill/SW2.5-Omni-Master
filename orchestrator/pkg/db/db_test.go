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
