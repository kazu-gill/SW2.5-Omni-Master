package game

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/kf/sw25-omni-master/pkg/db"
	"github.com/kf/sw25-omni-master/pkg/llm"
	"gopkg.in/yaml.v3"
)

// ParseNPCAction parses the YAML output from an NPC agent.
func ParseNPCAction(yamlText string) (*NPCAction, error) {
	var a NPCAction
	if err := yaml.Unmarshal([]byte(yamlText), &a); err != nil {
		return nil, fmt.Errorf("YAML parse: %w", err)
	}
	return &a, nil
}

// ValidateNPCAction checks that an NPC action is structurally valid and
// within resource limits provided by the current NPC sheet values.
func ValidateNPCAction(action *NPCAction, currentHP, currentMP int) error {
	if action.Name == "" {
		return fmt.Errorf("name is required")
	}
	if action.Action == "" {
		return fmt.Errorf("action is required")
	}
	if action.Dialogue == "" {
		return fmt.Errorf("dialogue is required")
	}
	if action.MPCost < 0 {
		return fmt.Errorf("mp_cost cannot be negative")
	}
	if action.MPCost > currentMP {
		return fmt.Errorf("mp_cost %d exceeds current MP %d", action.MPCost, currentMP)
	}
	if action.HPDelta < -currentHP {
		return fmt.Errorf("hp_delta %d would reduce HP below 0", action.HPDelta)
	}
	return nil
}

// refusalPrefixes are openings of a model declining to answer.
var refusalPrefixes = []string{"申し訳ありませんが", "申し訳ございませんが", "I'm sorry", "I cannot", "I can't"}

// ValidateGMNarration rejects GM output that is empty, a refusal, or
// structured data that belongs to the NPC / Support stages.
func ValidateGMNarration(res llm.Result) error {
	text := strings.TrimSpace(res.Text)
	if text == "" {
		return fmt.Errorf("応答が空です")
	}
	for _, p := range refusalPrefixes {
		if strings.HasPrefix(text, p) {
			return fmt.Errorf("応答を拒否しています")
		}
	}
	if strings.HasPrefix(text, "{") || strings.HasPrefix(text, "[") ||
		(strings.Contains(text, "name:") && strings.Contains(text, "action:")) {
		return fmt.Errorf("情景描写ではなく YAML / JSON を出力しています")
	}
	return nil
}

// ValidateNPCActionFor checks an NPC action against the sheet of the NPC that
// was asked to act.
func ValidateNPCActionFor(action *NPCAction, sheet db.NPCSheet) error {
	if err := ValidateNPCAction(action, sheet.HP, sheet.MP); err != nil {
		return err
	}
	if action.Name != sheet.Name {
		return fmt.Errorf("name が %q になっています (%q として行動してください)", action.Name, sheet.Name)
	}
	if action.NewLane != "" {
		if _, ok := laneToPositionY(action.NewLane); !ok {
			return fmt.Errorf("new_lane %q は無効です (enemy / front / party のいずれか)", action.NewLane)
		}
	}
	return nil
}

// ParseResourceDeltas parses the Support stage's JSON and checks that it has
// exactly one entry per acting NPC, with values within that NPC's HP / MP.
func ParseResourceDeltas(raw string, actions []NPCAction, sheets []db.NPCSheet) ([]ResourceDelta, error) {
	var deltas []ResourceDelta
	if err := json.Unmarshal([]byte(stripCodeBlock(raw)), &deltas); err != nil {
		return nil, fmt.Errorf("JSON 配列として読めません: %w", err)
	}

	byName := make(map[string]db.NPCSheet, len(sheets))
	for _, s := range sheets {
		byName[s.Name] = s
	}
	acting := make(map[string]bool, len(actions))
	for _, a := range actions {
		acting[a.Name] = true
	}

	seen := make(map[string]bool, len(deltas))
	for _, d := range deltas {
		if !acting[d.NPCName] {
			return nil, fmt.Errorf("行動していない NPC %q が含まれています", d.NPCName)
		}
		if seen[d.NPCName] {
			return nil, fmt.Errorf("NPC %q が重複しています", d.NPCName)
		}
		seen[d.NPCName] = true
		s := byName[d.NPCName]
		if d.MPCost < 0 || d.MPCost > s.MP {
			return nil, fmt.Errorf("%s の mp_cost %d は 0〜%d の範囲外です", d.NPCName, d.MPCost, s.MP)
		}
		if d.HPDelta < -s.HP {
			return nil, fmt.Errorf("%s の hp_delta %d は HP %d を超えています", d.NPCName, d.HPDelta, s.HP)
		}
	}
	for name := range acting {
		if !seen[name] {
			return nil, fmt.Errorf("NPC %q の結果がありません", name)
		}
	}
	return deltas, nil
}
