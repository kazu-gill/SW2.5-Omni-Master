package game

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"regexp"
	"strings"
	"time"

	"github.com/kf/sw25-omni-master/pkg/db"
	"github.com/kf/sw25-omni-master/pkg/llm"
	"github.com/kf/sw25-omni-master/pkg/rag"
)

// Engine orchestrates a full TRPG turn. Every stage runs on the same chat
// model, one call at a time, as one growing conversation (see transcript)
// that starts from the session context (see loadContext).
type Engine struct {
	db         *sql.DB
	llm        ChatModel
	rag        *rag.Engine
	retryLimit int // re-think attempts per stage after the first answer
	d6         func() int
}

func NewEngine(database *sql.DB, chat ChatModel, ragEngine *rag.Engine, retryLimit int) *Engine {
	return &Engine{
		db: database, llm: chat, rag: ragEngine, retryLimit: retryLimit,
		d6: func() int { return rand.IntN(6) + 1 },
	}
}

// SetDice replaces the die (tests).
func (e *Engine) SetDice(d6 func() int) { e.d6 = d6 }

// turnRun is one turn in progress.
type turnRun struct {
	e     *Engine
	ev    Events
	tr    *TurnResult
	tx    *transcript
	facts []string // 判定結果: told to every later stage of the turn
}

func (r *turnRun) stage() stageRun {
	return stageRun{sessionID: r.tr.SessionID, turn: r.tr.Turn, ev: r.ev}
}

// ProcessTurn executes one game turn:
//  1. Save a checkpoint (the state before this turn)
//  2. GM narration (with rules from RAG); checks the GM asks for are rolled
//  3. NPC actions, one NPC after another; their checks are rolled
//  4. Support confirms HP / MP changes
//  5. DB update
//
// A GM appeal (ModeGM) runs only step 2 and changes nothing. Each LLM stage
// is validated and re-thought on failure (see runStage); progress goes to ev.
func (e *Engine) ProcessTurn(ctx context.Context, input PlayerInput, ev Events) (*TurnResult, error) {
	if ev == nil {
		ev = NoEvents{}
	}
	if input.Mode == "" {
		input.Mode = ModeAction
	}
	if err := e.saveCheckpoint(ctx, input.SessionID, input.Turn); err != nil {
		return nil, fmt.Errorf("checkpoint: %w", err)
	}
	_ = db.AppendLog(e.db, input.SessionID, input.Turn, "player", input.Text)

	sheets, err := db.GetNPCSheets(e.db, input.SessionID)
	if err != nil {
		return nil, fmt.Errorf("get npc sheets: %w", err)
	}
	sc, err := e.loadContext(input.SessionID, sheets)
	if err != nil {
		return nil, fmt.Errorf("session context: %w", err)
	}
	run := &turnRun{
		e: e, ev: ev, tx: newTranscript(sc.messages()),
		tr: &TurnResult{
			SessionID: input.SessionID, Turn: input.Turn, Mode: input.Mode, Input: input.Text,
			StageAttempts: map[string]int{}, CreatedAt: time.Now(),
			// Empty lists, not null, so the UI can map over them.
			NPCActions: []NPCAction{}, Deltas: []ResourceDelta{}, FormationChanges: []FormationChange{},
			Checks: []Check{}, RulesUsed: []RuleHit{}, Warnings: []string{},
		},
	}

	gmNarration, err := run.gm(ctx, input, sheets)
	if err != nil {
		return nil, fmt.Errorf("gm narration: %w", err)
	}
	run.tr.GMNarration = gmNarration
	if input.Mode == ModeGM {
		return run.tr, nil
	}

	npcActions, err := run.npcs(ctx, sheets)
	if err != nil {
		return nil, fmt.Errorf("npc actions: %w", err)
	}
	run.tr.NPCActions = append(run.tr.NPCActions, npcActions...)

	deltas, err := run.support(ctx, npcActions, sheets)
	if err != nil {
		return nil, fmt.Errorf("resource calc: %w", err)
	}
	run.tr.Deltas = append(run.tr.Deltas, deltas...)

	if err := e.applyDeltas(deltas, sheets); err != nil {
		return nil, fmt.Errorf("apply deltas: %w", err)
	}
	run.tr.FormationChanges = append(run.tr.FormationChanges, e.applyFormationChanges(npcActions, sheets)...)
	return run.tr, nil
}

// Number of rulebook chunks given to each stage.
const (
	gmRuleCount      = 5
	supportRuleCount = 3
)

// rules searches the rulebook for query, reports what it found and returns
// the formatted context ("【参照ルール】…"). A failed or missing search returns
// "" — a missing rulebook must not stop the game.
func (r *turnRun) rules(ctx context.Context, stageKey, query string, topK int) string {
	e := r.e
	if e.rag == nil || strings.TrimSpace(query) == "" {
		return ""
	}
	chunks, err := e.rag.Search(ctx, query, topK)
	if err != nil || len(chunks) == 0 {
		return ""
	}
	hits := make([]RuleHit, 0, len(chunks))
	for _, c := range chunks {
		hits = append(hits, RuleHit{
			ID: c.ID, SourceType: c.SourceType, Section: c.Section, PageFrom: c.PageFrom, PageTo: c.PageTo,
			Text: c.Text, Score: c.Score, MatchedTerm: c.MatchedTerm,
		})
	}
	r.tr.RulesUsed = append(r.tr.RulesUsed, hits...)
	r.ev.Rules(r.tr.Turn, stageKey, hits)
	return e.rag.BuildContext(chunks)
}

// roll makes a check and tells the later stages and the UI about it.
func (r *turnRun) roll(who, kind string, base, target int) {
	c := rollCheck(who, kind, base, target, r.e.d6)
	r.tr.Checks = append(r.tr.Checks, c)
	r.facts = append(r.facts, c.Line())
	r.ev.Check(r.tr.Turn, c)
	_ = db.AppendLog(r.e.db, r.tr.SessionID, r.tr.Turn, "check", c.Line())
}

// factsBlock is the checks rolled so far this turn, for the next instruction.
func (r *turnRun) factsBlock() string {
	if len(r.facts) == 0 {
		return ""
	}
	return "【判定結果（システムが振ったサイコロ）】\n- " + strings.Join(r.facts, "\n- ") + "\n\n"
}

func (e *Engine) saveCheckpoint(ctx context.Context, sessionID int64, turn int) error {
	sheets, _ := db.GetNPCSheets(e.db, sessionID)
	snap, _ := json.Marshal(map[string]any{
		"session_id": sessionID,
		"turn":       turn,
		"npc_sheets": sheets,
	})
	return db.SaveCheckpoint(e.db, sessionID, turn, string(snap))
}

func partyStatus(sheets []db.NPCSheet) string {
	if len(sheets) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("【パーティの状態】\n")
	for _, s := range sheets {
		fmt.Fprintf(&b, "- %s: HP %d/%d, MP %d/%d, レーン: %s\n", s.Name, s.HP, s.HPMax, s.MP, s.MPMax, positionToLane(s.PositionY))
	}
	return b.String() + "\n"
}

const gmActionInstruction = `【指示: GM】
あなたはGMです。上のプレイヤーの行動に対して、情景と結果を2〜4文で描写してください。
- 行動の成否がサイコロで決まる場合は、成否を書かずに、最後の行に「判定: PC名/判定名/基準値N/目標値M」と書いてください（複数なら1行ずつ）。成否は次のターンで描写します。
- 前のターンに判定結果がある場合は、その成否に沿って描写してください。
- NPCの行動や数値はこの後の担当が決めるので、GMの地の文だけを書いてください。
- 空の返答は禁止です。`

const gmAppealInstruction = `【指示: GM（直訴への回答）】
プレイヤーからGMへの質問・相談です。ゲーム内の時間は進めず、GMとして直接答えてください。
- ルールの質問には参照ルールに沿って答える。状況の確認・NPCの紹介などには現状に沿って答える。
- NPCに行動させたり、判定を求めたりしないでください。
- 空の返答は禁止です。`

// gm opens the turn with the turn's context (rules, party, player input) and
// the GM's instruction. Later stages reuse this prefix.
func (r *turnRun) gm(ctx context.Context, input PlayerInput, sheets []db.NPCSheet) (string, error) {
	var b strings.Builder
	if rules := r.rules(ctx, "gm", input.Text, gmRuleCount); rules != "" {
		b.WriteString(rules + "\n")
	}
	b.WriteString(partyStatus(sheets))
	instr := gmActionInstruction
	label := "GM"
	if input.Mode == ModeGM {
		fmt.Fprintf(&b, "【プレイヤーからGMへの直訴】\n%s\n\n", input.Text)
		instr, label = gmAppealInstruction, "GM（直訴）"
	} else {
		fmt.Fprintf(&b, "【プレイヤーの行動】\n%s\n\n", input.Text)
	}
	b.WriteString(instr)
	instruction := b.String()

	var narration string
	var specs []checkSpec
	res, attempts, err := r.e.runStage(ctx, r.stage(), stage{
		name: "gm", label: label, stream: true,
		opts:      llm.CompletionOptions{Temperature: 0.8, MaxTokens: 2048},
		retryHint: "GMとして情景・結果を日本語の地の文で描写し直してください。",
		validate: func(res llm.Result) error {
			narration, specs = extractGMChecks(stripSpeakerLabel(res.Text))
			if input.Mode == ModeGM {
				specs = nil
			}
			return ValidateGMNarration(llm.Result{Text: narration, FinishReason: res.FinishReason})
		},
	}, r.tx.ask(instruction))
	r.tr.StageAttempts["gm"] = attempts
	if err != nil {
		return "", err
	}
	r.tx.record(instruction, res.Text)
	for _, c := range specs {
		r.roll(c.who, c.kind, c.base, c.target)
	}
	return narration, nil
}

// npcs asks each NPC for its action, one after another. An NPC whose output
// never validates sits out this turn (recorded in tr.Warnings); a transport
// error aborts the turn.
//
// Each NPC sees the GM narration, the checks rolled so far and the NPCs that
// already acted this turn through the shared transcript.
func (r *turnRun) npcs(ctx context.Context, sheets []db.NPCSheet) ([]NPCAction, error) {
	var actions []NPCAction
	for _, sheet := range sheets {
		instruction := r.factsBlock() + fmt.Sprintf(
			"【指示: NPC「%s」】\n"+
				"あなたはNPC「%s」です。人物設定の口調・価値観・行動原則に従ってください。現在HP=%d / MP=%d。現在のレーン: %s。\n"+
				"戦闘は3レーン制（enemy=敵後衛 / front=フロントライン / party=味方後衛）です。\n"+
				"GMの描写と、先に行動したNPCがいればその行動を受け、重ならない自分の行動を決めてください。\n"+
				"以下のYAML形式のみで行動を出力してください。コードブロック・説明文は一切不要。\n"+
				"制約: mp_costは0以上%d以下の整数、hp_deltaは%d以上0以下の整数。\n"+
				"new_laneは「不意打ちで後衛が前線に引き出された」「前線崩壊で押し込まれた」など"+
				"レーンが実際に変わる場合のみ enemy/front/party のいずれかを記入し、変化なければ省略。\n"+
				"checkは行動の成否がサイコロで決まる場合だけ「判定名 基準値N 目標値M」と書く（サイコロはシステムが振る）。不要なら省略。\n"+
				"name: %s\naction: <行動>\ndialogue: <台詞>\nmp_cost: <消費MP整数>\nhp_delta: <HP変化整数>\nnew_lane: <省略可>\ncheck: <省略可>",
			sheet.Name, sheet.Name, sheet.HP, sheet.MP, positionToLane(sheet.PositionY),
			sheet.MP, -sheet.HP,
			sheet.Name,
		)
		key := "npc:" + sheet.Name
		var action *NPCAction
		res, attempts, err := r.e.runStage(ctx, r.stage(), stage{
			name: "npc", key: key, label: sheet.Name, stream: true,
			opts: llm.CompletionOptions{Temperature: 0.7, MaxTokens: 2048},
			retryHint: fmt.Sprintf("name は %s、mp_costは0〜%d、hp_deltaは%d〜0の範囲でYAMLのみ再出力してください。",
				sheet.Name, sheet.MP, -sheet.HP),
			validate: func(res llm.Result) error {
				a, err := ParseNPCAction(stripCodeBlock(res.Text))
				if err == nil {
					err = ValidateNPCActionFor(a, sheet)
				}
				action = a
				return err
			},
		}, r.tx.ask(instruction))
		r.tr.StageAttempts[key] = attempts
		if errors.Is(err, errRejected) {
			r.tr.Warnings = append(r.tr.Warnings, fmt.Sprintf("NPC %s は行動できませんでした: %v", sheet.Name, err))
			r.ev.Stage(StageEvent{Turn: r.tr.Turn, Stage: key, Label: sheet.Name, Status: StageSkipped, Attempt: attempts})
			continue
		}
		if err != nil {
			return actions, fmt.Errorf("NPC %s: %w", sheet.Name, err)
		}
		r.tx.record(instruction, res.Text)
		// A check the code cannot read is not worth re-running the NPC for:
		// the action stands, the check is not rolled, and the player is told.
		if action.Check != "" {
			if kind, base, target, err := parseCheck(action.Check); err == nil {
				r.roll(sheet.Name, kind, base, target)
			} else {
				r.tr.Warnings = append(r.tr.Warnings, fmt.Sprintf("%s の判定を振れませんでした: %v", sheet.Name, err))
			}
		}
		actions = append(actions, *action)
	}
	return actions, nil
}

var speakerLabelRe = regexp.MustCompile(`^\s*(?:【GM】|GM\s*[:：])\s*`)

// stripSpeakerLabel drops a leading "GM:" the model copies from the recaps
// of earlier turns.
func stripSpeakerLabel(s string) string {
	return speakerLabelRe.ReplaceAllString(s, "")
}

// stripCodeBlock removes markdown code fences (```yaml ... ``` or ``` ... ```) from LLM output.
func stripCodeBlock(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```") {
		end := strings.LastIndex(s, "```")
		if end > 3 {
			inner := s[3:end]
			// skip optional language tag on first line
			if nl := strings.IndexByte(inner, '\n'); nl >= 0 {
				inner = inner[nl+1:]
			}
			return strings.TrimSpace(inner)
		}
	}
	return s
}

// support asks the Support stage to confirm each NPC's HP / MP change. If its
// output never validates, the NPCs' own values are used.
func (r *turnRun) support(ctx context.Context, actions []NPCAction, sheets []db.NPCSheet) ([]ResourceDelta, error) {
	if len(actions) == 0 {
		return nil, nil
	}

	actionsJSON, _ := json.Marshal(actions)
	instruction := r.factsBlock() + `【指示: 数値計算担当】
あなたはTRPGの数値計算担当です。
このターンのNPC行動リストと判定結果を受け取り、各NPCのHP変化とMP消費を確定してください。
判定に失敗した行動の効果は発生しません（消費MPは支払います）。
必ずJSON配列のみを返してください。説明文・コードブロック・改行以外の余分な文字は一切不要です。
形式（厳守）: [{"npc_name":"名前","hp_delta":0,"mp_cost":0}]
行動に攻撃を受けていなければhp_deltaは0、魔法でなければmp_costは0。`
	// 行動 (魔法名・特技名など) に関係するルールを引き、消費MPなどの確定に使わせる
	var query strings.Builder
	for _, a := range actions {
		fmt.Fprintf(&query, "%s %s\n", a.Action, a.Check)
	}
	if rules := r.rules(ctx, "support", query.String(), supportRuleCount); rules != "" {
		instruction += "\n魔法の消費MPなど、参照ルールに記載がある値はそれに従ってください。\n\n" + rules
	}
	instruction += "\n\nNPC行動リスト:\n" + string(actionsJSON)

	var deltas []ResourceDelta
	_, attempts, err := r.e.runStage(ctx, r.stage(), stage{
		name: "support", label: "数値確認",
		opts:      llm.CompletionOptions{MaxTokens: 512},
		retryHint: "行動リストの全NPCについて1件ずつ、指定形式のJSON配列のみを再出力してください。",
		validate: func(res llm.Result) error {
			d, err := ParseResourceDeltas(res.Text, actions, sheets)
			deltas = d
			return err
		},
	}, r.tx.ask(instruction))
	r.tr.StageAttempts["support"] = attempts
	if errors.Is(err, errRejected) {
		// Fallback: use values directly from NPCAction
		r.tr.Warnings = append(r.tr.Warnings, fmt.Sprintf("数値確認の出力が不正なため NPC 自身の値を使いました: %v", err))
		deltas = nil
		for _, a := range actions {
			deltas = append(deltas, ResourceDelta{NPCName: a.Name, HPDelta: a.HPDelta, MPCost: a.MPCost})
		}
		return deltas, nil
	}
	if err != nil {
		return nil, err
	}
	return deltas, nil
}

func positionToLane(y int) string {
	switch {
	case y <= 2:
		return "enemy"
	case y <= 5:
		return "front"
	default:
		return "party"
	}
}

func laneToPositionY(lane string) (int, bool) {
	switch lane {
	case "enemy":
		return 1, true
	case "front":
		return 4, true
	case "party":
		return 7, true
	default:
		return 0, false
	}
}

func (e *Engine) applyFormationChanges(actions []NPCAction, sheets []db.NPCSheet) []FormationChange {
	sheetMap := make(map[string]db.NPCSheet, len(sheets))
	for _, s := range sheets {
		sheetMap[s.Name] = s
	}
	var changes []FormationChange
	for _, a := range actions {
		if a.NewLane == "" {
			continue
		}
		newY, ok := laneToPositionY(a.NewLane)
		if !ok {
			continue
		}
		s, ok := sheetMap[a.Name]
		if !ok {
			continue
		}
		if positionToLane(s.PositionY) == a.NewLane {
			continue // no actual change
		}
		s.PositionY = newY
		_ = db.UpsertNPCSheet(e.db, s)
		changes = append(changes, FormationChange{Name: a.Name, NewLane: a.NewLane})
	}
	return changes
}

func (e *Engine) applyDeltas(deltas []ResourceDelta, sheets []db.NPCSheet) error {
	sheetMap := make(map[string]db.NPCSheet, len(sheets))
	for _, s := range sheets {
		sheetMap[s.Name] = s
	}
	for _, d := range deltas {
		s, ok := sheetMap[d.NPCName]
		if !ok {
			continue
		}
		s.HP += d.HPDelta
		s.MP -= d.MPCost
		if s.HP < 0 {
			s.HP = 0
		}
		if s.MP < 0 {
			s.MP = 0
		}
		if err := db.UpsertNPCSheet(e.db, s); err != nil {
			return err
		}
	}
	return nil
}
