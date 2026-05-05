package game

import (
	"fmt"

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
