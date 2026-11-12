package game

// Stage statuses reported through Events.
const (
	StageStart   = "start"
	StageRetry   = "retry" // the stage's text so far is discarded; it is generated again
	StageDone    = "done"
	StageFailed  = "failed"
	StageSkipped = "skipped" // an NPC whose output never validated sits out
)

// StageEvent reports progress of one LLM stage of a turn.
type StageEvent struct {
	Turn    int
	Stage   string // "gm", "npc:<名前>", "support", "summary"
	Label   string // 表示名
	Status  string
	Attempt int
	Message string
}

// Events receives a turn's progress as it happens, so the UI can show which
// stage is running and stream the text instead of waiting a minute in silence.
type Events interface {
	Stage(StageEvent)
	Delta(turn int, stage, text string)
	Rules(turn int, stage string, hits []RuleHit)
	Check(turn int, c Check)
}

// NoEvents discards progress.
type NoEvents struct{}

func (NoEvents) Stage(StageEvent)             {}
func (NoEvents) Delta(int, string, string)    {}
func (NoEvents) Rules(int, string, []RuleHit) {}
func (NoEvents) Check(int, Check)             {}
