package game

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Check results.
const (
	CheckSuccess     = "success"
	CheckFailure     = "failure"
	CheckAutoSuccess = "auto_success" // 出目 12
	CheckAutoFailure = "auto_failure" // 出目 2
)

// Check is one 2d6 roll made by the system. The LLM only says what to roll
// (判定の種類・基準値・目標値); the dice and the outcome are decided here, so
// the model can never fudge a roll.
type Check struct {
	Who    string `json:"who"`
	Kind   string `json:"kind"`
	Base   int    `json:"base"`
	Target int    `json:"target"`
	Dice   [2]int `json:"dice"`
	Total  int    `json:"total"`
	Result string `json:"result"`
}

var resultLabels = map[string]string{
	CheckSuccess: "成功", CheckFailure: "失敗", CheckAutoSuccess: "自動成功", CheckAutoFailure: "自動失敗",
}

// Line is the check as told to the next stages and stored in the log.
func (c Check) Line() string {
	return fmt.Sprintf("%s %s: 2d6[%d,%d]%+d=%d / 目標値%d → %s",
		c.Who, c.Kind, c.Dice[0], c.Dice[1], c.Base, c.Total, c.Target, resultLabels[c.Result])
}

// rollCheck rolls 2d6 with d6 and applies SW2.5's rules: 6-6 always
// succeeds, 1-1 always fails, otherwise base + dice must reach the target.
func rollCheck(who, kind string, base, target int, d6 func() int) Check {
	c := Check{Who: who, Kind: kind, Base: base, Target: target, Dice: [2]int{d6(), d6()}}
	sum := c.Dice[0] + c.Dice[1]
	c.Total = sum + base
	switch {
	case sum == 12:
		c.Result = CheckAutoSuccess
	case sum == 2:
		c.Result = CheckAutoFailure
	case c.Total >= target:
		c.Result = CheckSuccess
	default:
		c.Result = CheckFailure
	}
	return c
}

// Gemma often drops the 基準値/目標値 labels ("魔物知識 10 12") or uses
// slashes ("命中力判定/5/12"), so the labels are optional and the separator
// may be a space or a slash. Full-width digits and signs are normalized first.
var checkRe = regexp.MustCompile(`^\s*(.+?)\s*[/／]?\s*(?:基準値)?\s*[:：]?\s*([+-]?\d+)\s*[/／]?\s*(?:目標値)?\s*[:：]?\s*(\d+)\s*$`)

var widthFold = strings.NewReplacer(
	"０", "0", "１", "1", "２", "2", "３", "3", "４", "4",
	"５", "5", "６", "6", "７", "7", "８", "8", "９", "9", "＋", "+", "－", "-", "−", "-",
)

// parseCheck reads "判定名 基準値N 目標値M"; the labels may be left out.
func parseCheck(s string) (kind string, base, target int, err error) {
	m := checkRe.FindStringSubmatch(widthFold.Replace(s))
	if m == nil || strings.TrimSpace(m[1]) == "" {
		return "", 0, 0, fmt.Errorf("判定 %q を読めませんでした (「判定名 基準値N 目標値M」の形で書く)", s)
	}
	base, _ = strconv.Atoi(m[2])
	target, _ = strconv.Atoi(m[3])
	return strings.TrimSpace(m[1]), base, target, nil
}

type checkSpec struct {
	who, kind    string
	base, target int
}

var gmCheckLineRe = regexp.MustCompile(`^\s*判定\s*[:：]\s*(.+?)\s*[/／]\s*(.+)$`)

// extractGMChecks takes the "判定: 誰/判定名/基準値N/目標値M" lines out of
// the GM's narration and returns the narration without them. A 判定 line
// without numbers (e.g. "自動成功") is dropped from the narration but not
// rolled.
func extractGMChecks(text string) (string, []checkSpec) {
	var keep []string
	var specs []checkSpec
	for _, line := range strings.Split(text, "\n") {
		m := gmCheckLineRe.FindStringSubmatch(line)
		if m == nil {
			keep = append(keep, line)
			continue
		}
		if kind, base, target, err := parseCheck(m[2]); err == nil {
			specs = append(specs, checkSpec{who: m[1], kind: kind, base: base, target: target})
		}
	}
	return strings.TrimSpace(strings.Join(keep, "\n")), specs
}
