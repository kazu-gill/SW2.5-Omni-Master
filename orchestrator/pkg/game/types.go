package game

// PlayerInput represents a player's action declaration for a turn.
type PlayerInput struct {
	SessionID int64  `json:"session_id"`
	Turn      int    `json:"turn"`
	Text      string `json:"text"`
}

// NPCAction is the structured output expected from each NPC agent.
type NPCAction struct {
	Name     string `yaml:"name"`
	Action   string `yaml:"action"`
	Dice     string `yaml:"dice,omitempty"`
	Target   string `yaml:"target,omitempty"`
	Dialogue string `yaml:"dialogue"`
	HPDelta  int    `yaml:"hp_delta,omitempty"`
	MPCost   int    `yaml:"mp_cost,omitempty"`
	// NewLane is set when the NPC's position changes due to combat events.
	// Valid values: "enemy" (enemy rear), "front" (front line), "party" (party rear).
	NewLane string `yaml:"new_lane,omitempty"`
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
	GMNarration      string            `json:"gm_narration"`
	NPCActions       []NPCAction       `json:"npc_actions"`
	Deltas           []ResourceDelta   `json:"deltas"`
	FormationChanges []FormationChange `json:"formation_changes,omitempty"`
	ImageURL         string            `json:"image_url,omitempty"`
}
