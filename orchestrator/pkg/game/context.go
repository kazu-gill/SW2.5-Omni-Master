package game

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/kf/sw25-omni-master/pkg/db"
	"github.com/kf/sw25-omni-master/pkg/llm"
	"gopkg.in/yaml.v3"
)

// recentTurns is how many finished turns stay out of the summary. Every turn
// not yet summarized is sent word for word: between summaries that is
// recentTurns to recentTurns+summaryEvery-1 turns. Older turns reach the
// model only through the session summary, so a long session costs about the
// same per turn as a short one (the approach the MLX chat settled on).
const recentTurns = 2

// tableSystemPrompt opens every request. With the session's fixed context
// (quest, PC, NPC personas) after it, the whole system message stays the same
// from turn to turn, so mlx-vlm's prefix cache (APC) can reuse it; anything
// that changes every turn goes in the last user message instead.
const tableSystemPrompt = `あなたはソード・ワールド2.5のセッションを進行するAIです。
1ターンの中で、GM → 各NPC → 数値計算担当 の順に役を交代して応答します。
直前のユーザーメッセージの【指示】に書かれた役と出力形式だけに従い、その役の出力だけを返してください。
- プレイヤーの行動はプレイヤーキャラクター（PC）のものです。NPCの行動に書き換えないでください。
- サイコロはシステムが振ります。出目や判定の成否を自分で決めないでください。
- NPCは下の人物設定（口調・価値観・行動原則）に従って振る舞います。
- それより前の会話は、これまでのターンと同じターンの経過です。他の役の発言を自分の出力として繰り返さないでください。
すべて日本語で応答してください。`

// sessionContext is what the model knows about the session besides the
// current turn: the fixed system message and the last few turns.
type sessionContext struct {
	system string
	recent []llm.Message
}

func (c sessionContext) messages() []llm.Message {
	msgs := []llm.Message{{Role: "system", Content: c.system}}
	return append(msgs, c.recent...)
}

// loadContext builds the session context. Its parts are ordered from the one
// that changes least (table rules) to the one that changes most (summary), so
// a change near the end keeps the cache for everything before it.
func (e *Engine) loadContext(sessionID int64, sheets []db.NPCSheet) (sessionContext, error) {
	var b strings.Builder
	b.WriteString(tableSystemPrompt)

	if s, err := db.GetSession(e.db, sessionID); err == nil && s.QuestID != "" {
		if id, err := strconv.ParseInt(s.QuestID, 10, 64); err == nil {
			if q, err := db.GetQuest(e.db, id); err == nil {
				b.WriteString("\n\n" + questBlock(q))
			}
		}
	}
	if pc, err := db.GetActivePlayerCharacter(e.db); err == nil && pc != nil {
		b.WriteString("\n\n" + pcBlock(pc))
	}
	if len(sheets) > 0 {
		b.WriteString("\n\n【NPC（パーティの仲間）】")
		for _, s := range sheets {
			b.WriteString("\n" + personaBlock(s))
		}
	}
	summary, err := db.ListSummary(e.db, sessionID)
	if err != nil {
		return sessionContext{}, err
	}
	if len(summary) > 0 {
		b.WriteString("\n\n【これまでのあらすじ】")
		for _, s := range summary {
			b.WriteString("\n- " + s)
		}
	}

	recent, err := e.recentTurnMessages(sessionID)
	if err != nil {
		return sessionContext{}, err
	}
	return sessionContext{system: b.String(), recent: recent}, nil
}

func questBlock(q *db.Quest) string {
	var b strings.Builder
	fmt.Fprintf(&b, "【受注中のクエスト】%s（ランク%s）", q.Title, q.Rank)
	if q.Client != "" {
		fmt.Fprintf(&b, "\n依頼人: %s", q.Client)
	}
	if q.Target != "" {
		fmt.Fprintf(&b, "\n目的: %s", q.Target)
	}
	if q.Reward != "" {
		fmt.Fprintf(&b, "\n報酬: %s", q.Reward)
	}
	if q.Description != "" {
		fmt.Fprintf(&b, "\n概要: %s", q.Description)
	}
	return b.String()
}

// pcSheet is the part of the PC sheet (ゆとシート形式, json_blob) the GM needs.
type pcSheet struct {
	Name            string `json:"name"`
	Race            string `json:"race"`
	Gender          string `json:"gender"`
	Age             string `json:"age"`
	AdventurerLevel int    `json:"adventurerLevel"`
	Classes         []struct {
		Name  string `json:"name"`
		Level int    `json:"level"`
	} `json:"classes"`
	Attrs map[string]struct {
		Base   int `json:"base"`
		Growth int `json:"growth"`
	} `json:"attrs"`
	HPMax       int    `json:"hpMax"`
	MPMax       int    `json:"mpMax"`
	CombatFeats string `json:"combatFeats"`
	Weapons     []struct {
		Name string `json:"name"`
	} `json:"weapons"`
	Armor  string `json:"armor"`
	Shield string `json:"shield"`
	Notes  string `json:"notes"`
}

var attrNames = []struct{ key, label string }{
	{"dex", "器用"}, {"agi", "敏捷"}, {"str", "筋力"}, {"vit", "生命"}, {"int", "知力"}, {"spr", "精神"},
}

func pcBlock(pc *db.PlayerCharacter) string {
	var s pcSheet
	_ = json.Unmarshal([]byte(pc.JSONBlob), &s)
	name := s.Name
	if name == "" {
		name = pc.Name
	}
	var b strings.Builder
	fmt.Fprintf(&b, "【PC（プレイヤーキャラクター）】%s", name)
	var profile []string
	for _, v := range []string{s.Race, s.Gender, s.Age} {
		if v != "" {
			profile = append(profile, v)
		}
	}
	if len(profile) > 0 {
		fmt.Fprintf(&b, "（%s）", strings.Join(profile, "・"))
	}
	if s.AdventurerLevel > 0 {
		fmt.Fprintf(&b, "\n冒険者レベル%d", s.AdventurerLevel)
	}
	var classes []string
	for _, c := range s.Classes {
		if c.Name != "" {
			classes = append(classes, fmt.Sprintf("%s%d", c.Name, c.Level))
		}
	}
	if len(classes) > 0 {
		b.WriteString(" / 技能: " + strings.Join(classes, "、"))
	}
	var attrs []string
	for _, a := range attrNames {
		if v, ok := s.Attrs[a.key]; ok {
			total := v.Base + v.Growth
			attrs = append(attrs, fmt.Sprintf("%s%d(+%d)", a.label, total, total/6))
		}
	}
	if len(attrs) > 0 {
		b.WriteString("\n能力値: " + strings.Join(attrs, " "))
	}
	if s.HPMax > 0 || s.MPMax > 0 {
		fmt.Fprintf(&b, "\n最大HP%d / 最大MP%d", s.HPMax, s.MPMax)
	}
	var gear []string
	for _, w := range s.Weapons {
		if w.Name != "" {
			gear = append(gear, w.Name)
		}
	}
	for _, v := range []string{s.Armor, s.Shield} {
		if v != "" {
			gear = append(gear, v)
		}
	}
	if len(gear) > 0 {
		b.WriteString("\n装備: " + strings.Join(gear, "、"))
	}
	if s.CombatFeats != "" {
		b.WriteString("\n戦闘特技: " + oneLine(s.CombatFeats))
	}
	if s.Notes != "" {
		b.WriteString("\nメモ: " + oneLine(s.Notes))
	}
	return b.String()
}

// personaBlock turns an NPC's persona YAML into the lines the model plays by.
// Unknown or missing keys are skipped; the sheet name always comes first.
func personaBlock(s db.NPCSheet) string {
	var p map[string]any
	_ = yaml.Unmarshal([]byte(s.YAMLBlob), &p)
	var b strings.Builder
	fmt.Fprintf(&b, "■%s", s.Name)
	var profile []string
	for _, k := range []string{"race", "gender", "age"} {
		if v := str(p[k]); v != "" {
			profile = append(profile, v)
		}
	}
	if len(profile) > 0 {
		fmt.Fprintf(&b, "（%s）", strings.Join(profile, "・"))
	}
	if cls := classList(p["classes"]); cls != "" {
		b.WriteString(" 技能: " + cls)
	}
	if hp := s.HPMax; hp > 0 {
		fmt.Fprintf(&b, " 最大HP%d / 最大MP%d", hp, s.MPMax)
	}
	if per, ok := p["personality"].(map[string]any); ok {
		for _, f := range []struct{ key, label string }{
			{"tone", "性格"}, {"speech_style", "口調"}, {"values", "価値観"}, {"fears", "恐れ"}, {"quirks", "癖"},
		} {
			if v := str(per[f.key]); v != "" {
				fmt.Fprintf(&b, "\n  %s: %s", f.label, v)
			}
		}
	}
	if v := str(p["backstory"]); v != "" {
		b.WriteString("\n  経歴: " + v)
	}
	if rules, ok := p["action_rules"].([]any); ok && len(rules) > 0 {
		b.WriteString("\n  行動原則:")
		for _, r := range rules {
			b.WriteString("\n   - " + str(r))
		}
	}
	if eq, ok := p["equipment"].(map[string]any); ok {
		var gear []string
		for _, k := range []string{"weapon", "armor", "shield", "accessory"} {
			if v := str(eq[k]); v != "" {
				gear = append(gear, v)
			}
		}
		if len(gear) > 0 {
			b.WriteString("\n  装備: " + strings.Join(gear, "、"))
		}
	}
	if skills, ok := p["skills"].([]any); ok && len(skills) > 0 {
		var names []string
		for _, sk := range skills {
			// The description goes along: without it the model makes up
			// what a feat does when the player asks.
			if m, ok := sk.(map[string]any); ok && str(m["name"]) != "" {
				name := str(m["name"])
				if d := str(m["description"]); d != "" {
					name += "（" + d + "）"
				}
				names = append(names, name)
			}
		}
		if len(names) > 0 {
			b.WriteString("\n  特技: " + strings.Join(names, "、"))
		}
	}
	return b.String()
}

func classList(v any) string {
	list, ok := v.([]any)
	if !ok {
		return ""
	}
	var out []string
	for _, c := range list {
		if m, ok := c.(map[string]any); ok && str(m["name"]) != "" {
			out = append(out, str(m["name"])+str(m["level"]))
		}
	}
	return strings.Join(out, "、")
}

func str(v any) string {
	if v == nil {
		return ""
	}
	return oneLine(fmt.Sprint(v))
}

func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// recentTurnMessages returns the finished turns the summary does not cover
// yet as a player message and a recap answer each, oldest first.
func (e *Engine) recentTurnMessages(sessionID int64) ([]llm.Message, error) {
	results, err := db.ListTurnResults(e.db, sessionID)
	if err != nil {
		return nil, err
	}
	upto, err := db.SummarizedUpto(e.db, sessionID)
	if err != nil {
		return nil, err
	}
	// A summary that keeps failing must not let the request grow without end.
	if limit := recentTurns + summaryEvery; len(results) > limit {
		results = results[len(results)-limit:]
	}
	var msgs []llm.Message
	for _, raw := range results {
		var tr TurnResult
		if json.Unmarshal([]byte(raw), &tr) != nil || tr.Input == "" || tr.Turn <= upto {
			continue
		}
		msgs = append(msgs,
			llm.Message{Role: "user", Content: playerLine(tr)},
			llm.Message{Role: "assistant", Content: turnRecap(tr)},
		)
	}
	return msgs, nil
}

func playerLine(tr TurnResult) string {
	if tr.Mode == ModeGM {
		return fmt.Sprintf("【ターン%d・GMへの直訴】%s", tr.Turn, tr.Input)
	}
	return fmt.Sprintf("【ターン%d・プレイヤーの行動】%s", tr.Turn, tr.Input)
}

// turnRecap is how a finished turn reads in later requests: the GM's
// narration, each NPC as 名前「台詞」（行動）, and the checks rolled.
func turnRecap(tr TurnResult) string {
	var b strings.Builder
	b.WriteString("GM: " + tr.GMNarration)
	for _, a := range tr.NPCActions {
		fmt.Fprintf(&b, "\n%s「%s」（%s）", a.Name, strings.Trim(a.Dialogue, "「」"), a.Action)
	}
	for _, c := range tr.Checks {
		b.WriteString("\n判定結果: " + c.Line())
	}
	return b.String()
}
