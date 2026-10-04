package game

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/kf/sw25-omni-master/pkg/db"
	"github.com/kf/sw25-omni-master/pkg/llm"
	"github.com/kf/sw25-omni-master/pkg/rag"
)

// Engine orchestrates a full TRPG turn. Every stage runs on the same chat
// model, one call at a time, as one growing conversation (see transcript).
type Engine struct {
	db         *sql.DB
	llm        ChatModel
	rag        *rag.Engine
	retryLimit int // re-think attempts per stage after the first answer
}

func NewEngine(database *sql.DB, chat ChatModel, ragEngine *rag.Engine, retryLimit int) *Engine {
	return &Engine{db: database, llm: chat, rag: ragEngine, retryLimit: retryLimit}
}

// ProcessTurn executes one full game turn:
//  1. Save checkpoint
//  2. GM narration (with RAG context)
//  3. NPC actions, one NPC after another
//  4. Support LM resource calculation
//  5. DB update
//
// Each LLM stage is validated and re-thought on failure (see runStage).
func (e *Engine) ProcessTurn(ctx context.Context, input PlayerInput) (*TurnResult, error) {
	// 1. Checkpoint
	if err := e.saveCheckpoint(ctx, input.SessionID, input.Turn); err != nil {
		return nil, fmt.Errorf("checkpoint: %w", err)
	}

	// Log player input
	_ = db.AppendLog(e.db, input.SessionID, input.Turn, "player", input.Text)

	tr := &TurnResult{
		SessionID:     input.SessionID,
		Turn:          input.Turn,
		StageAttempts: map[string]int{},
	}

	sheets, err := db.GetNPCSheets(e.db, input.SessionID)
	if err != nil {
		return nil, fmt.Errorf("get npc sheets: %w", err)
	}
	tx := newTranscript()

	// 2. GM narration
	gmNarration, err := e.gmNarrate(ctx, tr, tx, input, sheets, e.rulesFor(ctx, input.Text, gmRuleCount))
	if err != nil {
		return nil, fmt.Errorf("gm narration: %w", err)
	}
	tr.GMNarration = gmNarration

	// 3. Let each NPC act in turn
	npcActions, err := e.runNPCs(ctx, tr, tx, sheets)
	if err != nil {
		return nil, fmt.Errorf("npc actions: %w", err)
	}
	tr.NPCActions = npcActions

	// 4. Support LM resource calculation
	deltas, err := e.calculateResources(ctx, tr, tx, npcActions, sheets)
	if err != nil {
		return nil, fmt.Errorf("resource calc: %w", err)
	}
	tr.Deltas = deltas

	// 5. Apply deltas to DB
	if err := e.applyDeltas(deltas, sheets); err != nil {
		return nil, fmt.Errorf("apply deltas: %w", err)
	}

	// Apply formation changes from NPC actions
	tr.FormationChanges = e.applyFormationChanges(npcActions, sheets)

	return tr, nil
}

// Number of rulebook chunks given to each stage.
const (
	gmRuleCount      = 5
	supportRuleCount = 3
)

// rulesFor searches the rulebook for query and returns the formatted context
// ("【参照ルール】…"), or "" when there is no RAG engine or the search fails —
// a missing rulebook must not stop the game.
func (e *Engine) rulesFor(ctx context.Context, query string, topK int) string {
	if e.rag == nil || strings.TrimSpace(query) == "" {
		return ""
	}
	chunks, err := e.rag.Search(ctx, query, topK)
	if err != nil {
		return ""
	}
	return e.rag.BuildContext(chunks)
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

// gmNarrate opens the turn's transcript with the turn context (rules, party,
// player input) and the GM's instruction. Later stages reuse this prefix.
func (e *Engine) gmNarrate(ctx context.Context, tr *TurnResult, tx *transcript, input PlayerInput, sheets []db.NPCSheet, ragCtx string) (string, error) {
	var b strings.Builder
	if ragCtx != "" {
		b.WriteString(ragCtx + "\n\n")
	}
	if len(sheets) > 0 {
		b.WriteString("【パーティ】\n")
		for _, s := range sheets {
			fmt.Fprintf(&b, "- %s: HP=%d / MP=%d / レーン: %s\n", s.Name, s.HP, s.MP, positionToLane(s.PositionY))
		}
		b.WriteString("\n")
	}
	fmt.Fprintf(&b, "【プレイヤー入力】\n%s\n\n", input.Text)
	b.WriteString(`【指示: GM】
あなたはGMです。上のプレイヤー入力に対して応答してください。
- 行動描写の場合: 情景・結果を2〜4文で描写する
- 質問や直訴の場合: その内容に直接答える（NPCの紹介、ルール説明、状況確認など）
- 必ず何かしら内容のある返答を返すこと。空の返答は禁止。
- NPCの行動や数値はこの後の担当が決めるので、GMの地の文だけを書く。`)
	instruction := b.String()

	res, attempts, err := e.runStage(ctx, tr.SessionID, tr.Turn, stage{
		name:      "gm",
		opts:      llm.CompletionOptions{Temperature: 0.8, MaxTokens: 2048},
		retryHint: "GMとして情景・結果を日本語の地の文で描写し直してください。",
		validate:  func(r llm.Result) error { return ValidateGMNarration(r) },
	}, tx.ask(instruction))
	tr.StageAttempts["gm"] = attempts
	if err != nil {
		return "", err
	}
	tx.record(instruction, res.Text)
	return strings.TrimSpace(res.Text), nil
}

// runNPCs asks each NPC for its action, one after another. An NPC whose
// output never validates sits out this turn (recorded in tr.Warnings); a
// transport error aborts the turn.
//
// Each NPC sees the GM narration and the NPCs that already acted this turn
// through the shared transcript.
func (e *Engine) runNPCs(ctx context.Context, tr *TurnResult, tx *transcript, sheets []db.NPCSheet) ([]NPCAction, error) {
	var actions []NPCAction
	for _, sheet := range sheets {
		currentLane := positionToLane(sheet.PositionY)
		instruction := fmt.Sprintf(
			"【指示: NPC「%s」】\n"+
				"あなたはNPC「%s」です。現在HP=%d / MP=%d。現在のレーン: %s。\n"+
				"戦闘は3レーン制（enemy=敵後衛 / front=フロントライン / party=味方後衛）です。\n"+
				"GMの描写と、先に行動したNPCがいればその行動を受け、重ならない自分の行動を決めてください。\n"+
				"以下のYAML形式のみで行動を出力してください。コードブロック・説明文は一切不要。\n"+
				"制約: mp_costは0以上%d以下の整数、hp_deltaは%d以上0以下の整数。\n"+
				"new_laneは「不意打ちで後衛が前線に引き出された」「前線崩壊で押し込まれた」など"+
				"レーンが実際に変わる場合のみ enemy/front/party のいずれかを記入し、変化なければ省略。\n"+
				"name: %s\naction: <行動>\ndialogue: <台詞>\nmp_cost: <消費MP整数>\nhp_delta: <HP変化整数>\nnew_lane: <省略可>",
			sheet.Name, sheet.Name, sheet.HP, sheet.MP, currentLane,
			sheet.MP, -sheet.HP,
			sheet.Name,
		)
		var action *NPCAction
		res, attempts, err := e.runStage(ctx, tr.SessionID, tr.Turn, stage{
			name: "npc",
			opts: llm.CompletionOptions{Temperature: 0.7, MaxTokens: 2048},
			retryHint: fmt.Sprintf("name は %s、mp_costは0〜%d、hp_deltaは%d〜0の範囲でYAMLのみ再出力してください。",
				sheet.Name, sheet.MP, -sheet.HP),
			validate: func(r llm.Result) error {
				a, err := ParseNPCAction(stripCodeBlock(r.Text))
				if err == nil {
					err = ValidateNPCActionFor(a, sheet)
				}
				action = a
				return err
			},
		}, tx.ask(instruction))
		tr.StageAttempts["npc:"+sheet.Name] = attempts
		if errors.Is(err, errRejected) {
			tr.Warnings = append(tr.Warnings, fmt.Sprintf("NPC %s は行動できませんでした: %v", sheet.Name, err))
			continue
		}
		if err != nil {
			return actions, fmt.Errorf("NPC %s: %w", sheet.Name, err)
		}
		tx.record(instruction, res.Text)
		actions = append(actions, *action)
	}
	return actions, nil
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

// calculateResources asks the Support stage to confirm each NPC's HP / MP
// change. If its output never validates, the NPCs' own values are used.
func (e *Engine) calculateResources(ctx context.Context, tr *TurnResult, tx *transcript, actions []NPCAction, sheets []db.NPCSheet) ([]ResourceDelta, error) {
	if len(actions) == 0 {
		return nil, nil
	}

	actionsJSON, _ := json.Marshal(actions)
	instruction := `【指示: 数値計算担当】
あなたはTRPGの数値計算担当です。
このターンのNPC行動リストを受け取り、各NPCのHP変化とMP消費を確定してください。
必ずJSON配列のみを返してください。説明文・コードブロック・改行以外の余分な文字は一切不要です。
形式（厳守）: [{"npc_name":"名前","hp_delta":0,"mp_cost":0}]
行動に攻撃がなければhp_deltaは0、魔法でなければmp_costは0。`
	// 行動 (魔法名・特技名など) に関係するルールを引き、消費MPなどの確定に使わせる
	var query strings.Builder
	for _, a := range actions {
		fmt.Fprintf(&query, "%s %s\n", a.Action, a.Dice)
	}
	if rules := e.rulesFor(ctx, query.String(), supportRuleCount); rules != "" {
		instruction += "\n魔法の消費MPなど、参照ルールに記載がある値はそれに従ってください。\n\n" + rules
	}
	instruction += "\n\nNPC行動リスト:\n" + string(actionsJSON)

	var deltas []ResourceDelta
	_, attempts, err := e.runStage(ctx, tr.SessionID, tr.Turn, stage{
		name:      "support",
		opts:      llm.CompletionOptions{MaxTokens: 512},
		retryHint: "行動リストの全NPCについて1件ずつ、指定形式のJSON配列のみを再出力してください。",
		validate: func(r llm.Result) error {
			d, err := ParseResourceDeltas(r.Text, actions, sheets)
			deltas = d
			return err
		},
	}, tx.ask(instruction))
	tr.StageAttempts["support"] = attempts
	if errors.Is(err, errRejected) {
		// Fallback: use values directly from NPCAction
		tr.Warnings = append(tr.Warnings, fmt.Sprintf("Support の出力が不正なため NPC 自身の値を使いました: %v", err))
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
