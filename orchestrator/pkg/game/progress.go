package game

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"github.com/kf/sw25-omni-master/pkg/db"
	"github.com/kf/sw25-omni-master/pkg/llm"
)

var (
	ErrNoStoppedTurn = errors.New("続きから再開できるターンがありません")
	ErrPartyChanged  = errors.New("止まった後にパーティが変わったため、続きから再開できません。取り消してください")
)

// turnState is a turn in progress as saved in turn_progress.state_json after
// every finished stage. The session context is not saved: it is rebuilt on
// resume, and a stopped turn keeps the session from changing.
type turnState struct {
	Done       []string      `json:"done"`       // "gm", "npc:<名前>", "support"
	Transcript []llm.Message `json:"transcript"` // 土台より後 (record した instruction/answer の組)
	Facts      []string      `json:"facts"`
	Result     TurnResult    `json:"result"`
	NPCs       []string      `json:"npcs"`     // 開始時の NPC 名
	LogMark    int64         `json:"log_mark"` // 最後に終わった段の時点の session_logs の最大 id
}

// Stage keys in turnState.Done.
const (
	doneGM      = "gm"
	doneSupport = "support"
)

func npcKey(name string) string { return "npc:" + name }

func npcNames(sheets []db.NPCSheet) []string {
	names := make([]string, 0, len(sheets))
	for _, s := range sheets {
		names = append(names, s.Name)
	}
	return names
}

func (r *turnRun) isDone(key string) bool { return slices.Contains(r.done, key) }

// markDone records a finished stage and saves the turn. Call it after the
// stage's results and checks are in r.tr / r.facts.
func (r *turnRun) markDone(key string) error {
	r.done = append(r.done, key)
	return r.saveProgress()
}

// saveProgress writes the turn as it stands (status running). LogMark is
// taken now, so the session_logs rows of a stage that stops later can be
// dropped before it is run again.
func (r *turnRun) saveProgress() error {
	mark, err := db.MaxLogID(r.e.db, r.tr.SessionID)
	if err != nil {
		return err
	}
	st := turnState{
		Done: r.done, Transcript: r.tx.added(), Facts: r.facts,
		Result: *r.tr, NPCs: r.party, LogMark: mark,
	}
	b, err := json.Marshal(st)
	if err != nil {
		return fmt.Errorf("marshal turn state: %w", err)
	}
	return db.SaveTurnProgress(r.e.db, db.TurnProgress{
		SessionID: r.tr.SessionID, Turn: r.tr.Turn, Input: r.tr.Input, Mode: r.tr.Mode,
		StateJSON: string(b), Status: db.ProgressRunning,
	})
}

// loadState reads a saved turnState and fills in what JSON leaves out.
func loadState(stateJSON string) (*turnState, error) {
	var st turnState
	if err := json.Unmarshal([]byte(stateJSON), &st); err != nil {
		return nil, fmt.Errorf("turn state: %w", err)
	}
	if st.Result.StageAttempts == nil { // omitempty drops an empty map
		st.Result.StageAttempts = map[string]int{}
	}
	return &st, nil
}
