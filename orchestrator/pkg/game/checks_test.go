package game

import "testing"

func fixedDice(rolls ...int) func() int {
	i := 0
	return func() int { r := rolls[i%len(rolls)]; i++; return r }
}

func TestRollCheck(t *testing.T) {
	tests := []struct {
		name   string
		dice   []int
		base   int
		target int
		want   string
		total  int
	}{
		{"success", []int{3, 4}, 5, 12, CheckSuccess, 12},
		{"failure", []int{3, 3}, 5, 12, CheckFailure, 11},
		{"six-six always succeeds", []int{6, 6}, 0, 99, CheckAutoSuccess, 12},
		{"one-one always fails", []int{1, 1}, 50, 3, CheckAutoFailure, 52},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := rollCheck("ガルド", "命中力判定", tt.base, tt.target, fixedDice(tt.dice...))
			if c.Result != tt.want || c.Total != tt.total || c.Dice != [2]int{tt.dice[0], tt.dice[1]} {
				t.Errorf("got %+v", c)
			}
		})
	}
}

func TestParseNPCCheck(t *testing.T) {
	kind, base, target, err := parseNPCCheck("命中力判定 基準値5 目標値12")
	if err != nil || kind != "命中力判定" || base != 5 || target != 12 {
		t.Errorf("got %q %d %d %v", kind, base, target, err)
	}
	if _, _, _, err := parseNPCCheck("命中力判定で攻撃"); err == nil {
		t.Error("expected error for a check without numbers")
	}
}

func TestExtractGMChecks(t *testing.T) {
	text := "ゴブリンが錆びた剣を振りかざした。あなたは身を翻そうとする。\n判定: ギル/回避力判定/基準値8/目標値11\n判定：ギル / 危険感知判定 / 基準値 6 / 目標値 10"
	narr, specs := extractGMChecks(text)
	if narr != "ゴブリンが錆びた剣を振りかざした。あなたは身を翻そうとする。" {
		t.Errorf("narration = %q", narr)
	}
	if len(specs) != 2 || specs[0].who != "ギル" || specs[0].kind != "回避力判定" || specs[0].base != 8 || specs[0].target != 11 ||
		specs[1].kind != "危険感知判定" || specs[1].base != 6 {
		t.Errorf("specs = %+v", specs)
	}
}

func TestCheckSummaryLine(t *testing.T) {
	c := Check{Who: "ガルド", Kind: "命中力判定", Base: 5, Target: 12, Dice: [2]int{3, 4}, Total: 12, Result: CheckSuccess}
	if got := c.Line(); got != "ガルド 命中力判定: 2d6[3,4]+5=12 / 目標値12 → 成功" {
		t.Errorf("Line = %q", got)
	}
}
