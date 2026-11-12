package game

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/kf/sw25-omni-master/pkg/db"
	"github.com/kf/sw25-omni-master/pkg/llm"
)

const gardYAML = `id: gard
name: ガルド
race: ドワーフ
classes:
  - name: ファイター
    level: 4
personality:
  tone: 無骨で短気。
  speech_style: 短文・体言止め。
backstory: |
  元傭兵。
action_rules:
  - 降伏した敵に止めを刺さない
`

// recordingEvents keeps what the engine reported.
type recordingEvents struct {
	stages []StageEvent
	deltas map[string]string
	checks []Check
	rules  map[string]int
}

func newRecording() *recordingEvents {
	return &recordingEvents{deltas: map[string]string{}, rules: map[string]int{}}
}
func (r *recordingEvents) Stage(s StageEvent)                     { r.stages = append(r.stages, s) }
func (r *recordingEvents) Delta(_ int, stage, text string)        { r.deltas[stage] += text }
func (r *recordingEvents) Rules(_ int, stage string, h []RuleHit) { r.rules[stage] += len(h) }
func (r *recordingEvents) Check(_ int, c Check)                   { r.checks = append(r.checks, c) }

func TestContextCarriesPersonaQuestPCAndRecentTurns(t *testing.T) {
	m := &scriptedLLM{replies: []llm.Result{{Text: "GMの描写。", FinishReason: "stop"}}}
	e := newStageEngine(t, m, 0)
	_ = db.SeedQuests(e.db)
	quests, _ := db.ListQuests(e.db)
	s, _ := db.CreateSessionTitled(e.db, "t", "")
	_ = db.SetSessionQuest(e.db, s.ID, jsonID(quests[0].ID))
	pc, _ := db.CreatePlayerCharacter(e.db, "ギル", `{"name":"ギル","race":"人間","adventurerLevel":3,"classes":[{"name":"フェンサー","level":3}]}`)
	_ = db.ActivatePlayerCharacter(e.db, pc.ID)
	_ = db.UpsertNPCSheet(e.db, db.NPCSheet{SessionID: s.ID, Name: "ガルド", HP: 30, MP: 10, HPMax: 38, MPMax: 10, YAMLBlob: gardYAML})
	for turn, input := range []string{"", "一つ目", "二つ目", "三つ目"} {
		if turn == 0 {
			continue
		}
		b, _ := json.Marshal(TurnResult{Turn: turn, Mode: ModeAction, Input: input, GMNarration: "描写" + input})
		_ = db.SaveTurnResult(e.db, s.ID, turn, string(b))
	}

	if _, err := e.ProcessTurn(context.Background(), PlayerInput{SessionID: s.ID, Turn: 4, Text: "質問", Mode: ModeGM}, nil); err != nil {
		t.Fatal(err)
	}
	msgs := m.calls[0].msgs
	system := msgs[0].Content
	for _, want := range []string{quests[0].Title, "【PC（プレイヤーキャラクター）】ギル", "フェンサー3", "■ガルド（ドワーフ）", "口調: 短文・体言止め。", "降伏した敵に止めを刺さない"} {
		if !strings.Contains(system, want) {
			t.Errorf("system prompt lacks %q:\n%s", want, system)
		}
	}
	var all []string
	for _, msg := range msgs[1:] {
		all = append(all, msg.Content)
	}
	joined := strings.Join(all, "\n")
	if strings.Contains(joined, "一つ目") || !strings.Contains(joined, "二つ目") || !strings.Contains(joined, "描写三つ目") {
		t.Errorf("only the last %d turns should be sent:\n%s", recentTurns, joined)
	}
}

func jsonID(id int64) string { b, _ := json.Marshal(id); return string(b) }

func TestGMAppealRunsOnlyTheGM(t *testing.T) {
	e, m, sid := setupSerialEngine(t, []llm.Result{{Text: "先制判定はスカウト技能で行います。", FinishReason: "stop"}}, "A")
	res, err := e.ProcessTurn(context.Background(), PlayerInput{SessionID: sid, Turn: 1, Text: "先制判定は？", Mode: ModeGM}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.calls) != 1 || len(res.NPCActions) != 0 || len(res.Deltas) != 0 || res.Mode != ModeGM {
		t.Errorf("calls=%d result=%+v", len(m.calls), res)
	}
	if !strings.Contains(m.calls[0].msgs[len(m.calls[0].msgs)-1].Content, "直訴") {
		t.Error("GM appeal should use the appeal instruction")
	}
}

func TestChecksAreRolledByCodeAndPassedOn(t *testing.T) {
	e, m, sid := setupSerialEngine(t, []llm.Result{
		{Text: "ゴブリンが飛びかかる。\n判定: ギル/回避力判定/基準値6/目標値11", FinishReason: "stop"},
		{Text: "name: A\naction: 斬りかかる\ndialogue: いくぞ\nmp_cost: 0\nhp_delta: 0\ncheck: 命中力判定 基準値5 目標値10", FinishReason: "stop"},
		{Text: `[{"npc_name":"A","hp_delta":0,"mp_cost":0}]`, FinishReason: "stop"},
	}, "A")
	e.SetDice(fixedDice(6, 5, 1, 2)) // 回避 6+5+6=17 成功 / 命中 1+2+5=8 失敗
	ev := newRecording()
	res, err := e.ProcessTurn(context.Background(), PlayerInput{SessionID: sid, Turn: 1, Text: "身構える"}, ev)
	if err != nil {
		t.Fatal(err)
	}
	if res.GMNarration != "ゴブリンが飛びかかる。" {
		t.Errorf("check line should be taken out of the narration: %q", res.GMNarration)
	}
	if len(res.Checks) != 2 || res.Checks[0].Result != CheckSuccess || res.Checks[1].Result != CheckFailure || res.Checks[1].Who != "A" {
		t.Fatalf("checks = %+v", res.Checks)
	}
	if len(ev.checks) != 2 {
		t.Errorf("events checks = %d", len(ev.checks))
	}
	npcInstr := m.calls[1].msgs[len(m.calls[1].msgs)-1].Content
	supportInstr := m.calls[2].msgs[len(m.calls[2].msgs)-1].Content
	if !strings.Contains(npcInstr, "ギル 回避力判定: 2d6[6,5]+6=17 / 目標値11 → 成功") {
		t.Errorf("NPC should see the GM check result:\n%s", npcInstr)
	}
	if !strings.Contains(supportInstr, "A 命中力判定: 2d6[1,2]+5=8 / 目標値10 → 失敗") {
		t.Errorf("support should see the NPC check result:\n%s", supportInstr)
	}
}

func TestNPCCheckMustBeWellFormed(t *testing.T) {
	e, _, sid := setupSerialEngine(t, []llm.Result{
		{Text: "GMの描写。", FinishReason: "stop"},
		{Text: "name: A\naction: 斬る\ndialogue: x\ncheck: 命中力判定で攻撃", FinishReason: "stop"},
		{Text: npcYAML("A"), FinishReason: "stop"},
		{Text: `[{"npc_name":"A","hp_delta":-5,"mp_cost":3}]`, FinishReason: "stop"},
	}, "A")
	res, err := e.ProcessTurn(context.Background(), PlayerInput{SessionID: sid, Turn: 1, Text: "進む"}, nil)
	if err != nil || res.StageAttempts["npc:A"] != 2 {
		t.Fatalf("attempts=%v err=%v", res.StageAttempts, err)
	}
}

func TestEventsReportStagesAndStreamedText(t *testing.T) {
	e, _, sid := setupSerialEngine(t, []llm.Result{
		{Text: "", FinishReason: "stop"},
		{Text: "GMの描写。", FinishReason: "stop"},
		{Text: npcYAML("A"), FinishReason: "stop"},
		{Text: `[{"npc_name":"A","hp_delta":-5,"mp_cost":3}]`, FinishReason: "stop"},
	}, "A")
	ev := newRecording()
	if _, err := e.ProcessTurn(context.Background(), PlayerInput{SessionID: sid, Turn: 1, Text: "進む"}, ev); err != nil {
		t.Fatal(err)
	}
	var seq []string
	for _, s := range ev.stages {
		seq = append(seq, s.Stage+":"+s.Status)
	}
	want := "gm:start gm:retry gm:done npc:A:start npc:A:done support:start support:done"
	if strings.Join(seq, " ") != want {
		t.Errorf("stages = %s\nwant     %s", strings.Join(seq, " "), want)
	}
}

func TestMaybeSummarizeWaitsForEnoughTurns(t *testing.T) {
	m := &scriptedLLM{replies: []llm.Result{{Text: "前置き\n- ガルドはゴブリンを倒した。\n- PCは鍵を拾った。", FinishReason: "stop"}}}
	e := newStageEngine(t, m, 0)
	s, _ := db.CreateSessionTitled(e.db, "t", "")
	save := func(turn int) {
		b, _ := json.Marshal(TurnResult{Turn: turn, Mode: ModeAction, Input: "行動", GMNarration: "描写"})
		_ = db.SaveTurnResult(e.db, s.ID, turn, string(b))
	}
	for turn := 1; turn <= 4; turn++ {
		save(turn)
	}
	if done, err := e.MaybeSummarize(context.Background(), s.ID, nil); done || err != nil || len(m.calls) != 0 {
		t.Fatalf("4 turns (2 recent) should not summarize yet: done=%v err=%v", done, err)
	}
	save(5)
	done, err := e.MaybeSummarize(context.Background(), s.ID, nil)
	if !done || err != nil {
		t.Fatalf("done=%v err=%v", done, err)
	}
	sum, _ := db.ListSummary(e.db, s.ID)
	upto, _ := db.SummarizedUpto(e.db, s.ID)
	if len(sum) != 2 || sum[0] != "ガルドはゴブリンを倒した。" || upto != 3 {
		t.Errorf("sum=%v upto=%d", sum, upto)
	}
}
