package game

import "time"

// Turn modes.
const (
	ModeAction = "action" // 行動宣言: GM → NPC → 数値確認
	ModeGM     = "gm"     // GM 直訴: GM だけが答え、NPC と数値は動かない
)

// PlayerInput represents a player's action declaration for a turn.
type PlayerInput struct {
	SessionID int64  `json:"session_id"`
	Turn      int    `json:"turn"`
	Text      string `json:"text"`
	Mode      string `json:"mode"` // ModeAction (default) or ModeGM
}

// NPCAction is the structured output expected from each NPC agent.
type NPCAction struct {
	Name     string `yaml:"name" json:"name"`
	Action   string `yaml:"action" json:"action"`
	Dice     string `yaml:"dice,omitempty" json:"-"`
	Target   string `yaml:"target,omitempty" json:"target,omitempty"`
	Dialogue string `yaml:"dialogue" json:"dialogue"`
	HPDelta  int    `yaml:"hp_delta,omitempty" json:"hp_delta"`
	MPCost   int    `yaml:"mp_cost,omitempty" json:"mp_cost"`
	// NewLane is set when the NPC's position changes due to combat events.
	// Valid values: "enemy" (enemy rear), "front" (front line), "party" (party rear).
	NewLane string `yaml:"new_lane,omitempty" json:"new_lane,omitempty"`
	// Check asks the system to roll: "命中力判定 基準値5 目標値12".
	Check string `yaml:"check,omitempty" json:"check,omitempty"`
}

// FormationChange records a lane change that happened during a turn.
type FormationChange struct {
	Name    string `json:"name"`
	NewLane string `json:"new_lane"`
}

// ResourceDelta is the confirmed numeric change computed by the support LM.
type ResourceDelta struct {
	NPCName string `json:"npc_name"`
	HPDelta int    `json:"hp_delta"`
	MPCost  int    `json:"mp_cost"`
}

// ImageUpdate is broadcast via WebSocket when a ComfyUI image is ready.
type ImageUpdate struct {
	SessionID int64  `json:"session_id"`
	Turn      int    `json:"turn"`
	ImageURL  string `json:"image_url"`
}

// TurnResult is the full output of one game turn.
type TurnResult struct {
	SessionID        int64             `json:"session_id"`
	Turn             int               `json:"turn"`
	Mode             string            `json:"mode"`
	Input            string            `json:"input"`
	GMNarration      string            `json:"gm_narration"`
	NPCActions       []NPCAction       `json:"npc_actions"`
	Deltas           []ResourceDelta   `json:"deltas"`
	FormationChanges []FormationChange `json:"formation_changes"`
	Checks           []Check           `json:"checks"`
	RulesUsed        []RuleHit         `json:"rules_used"`
	ImageURL         string            `json:"image_url,omitempty"`
	// StageAttempts counts LLM calls per stage ("gm", "npc:<name>", "support");
	// more than 1 means the stage was re-thought.
	StageAttempts map[string]int `json:"stage_attempts,omitempty"`
	// Warnings lists stages that never validated and were skipped or fell back.
	Warnings  []string  `json:"warnings"`
	CreatedAt time.Time `json:"created_at"`
}

// RuleHit is a rule passed to a stage, as shown to the player.
type RuleHit struct {
	ID          int64   `json:"id"`
	SourceType  string  `json:"source_type"`
	Section     string  `json:"section"`
	PageFrom    int     `json:"page_from,omitempty"`
	PageTo      int     `json:"page_to,omitempty"`
	Text        string  `json:"text"`
	Score       float64 `json:"score"`
	MatchedTerm string  `json:"matched_term,omitempty"`
}
