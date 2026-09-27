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
// model, one call at a time.
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

	// 2. GM narration
	var ragCtx string
	if e.rag != nil {
		// RAG failure is non-fatal; continue without context
		if ragChunks, err := e.rag.Search(ctx, input.Text, 5); err == nil {
			ragCtx = e.rag.BuildContext(ragChunks)
		}
	}
	gmNarration, err := e.gmNarrate(ctx, tr, input, ragCtx)
	if err != nil {
		return nil, fmt.Errorf("gm narration: %w", err)
	}
	tr.GMNarration = gmNarration

	// 3. Load NPC sheets and let each NPC act in turn
	sheets, err := db.GetNPCSheets(e.db, input.SessionID)
	if err != nil {
		return nil, fmt.Errorf("get npc sheets: %w", err)
	}
	npcActions, err := e.runNPCs(ctx, tr, gmNarration, sheets)
	if err != nil {
		return nil, fmt.Errorf("npc actions: %w", err)
	}
	tr.NPCActions = npcActions

	// 4. Support LM resource calculation
	deltas, err := e.calculateResources(ctx, tr, npcActions, sheets)
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

func (e *Engine) saveCheckpoint(ctx context.Context, sessionID int64, turn int) error {
	sheets, _ := db.GetNPCSheets(e.db, sessionID)
	snap, _ := json.Marshal(map[string]any{
		"session_id": sessionID,
		"turn":       turn,
		"npc_sheets": sheets,
	})
	return db.SaveCheckpoint(e.db, sessionID, turn, string(snap))
}

func (e *Engine) gmNarrate(ctx context.Context, tr *TurnResult, input PlayerInput, ragCtx string) (string, error) {
	systemPrompt := `あなたはソード・ワールド2.5のGMです。
プレイヤーやシステムからの入力に対して、必ず日本語で応答してください。
- 行動描写の場合: 情景・結果を2〜4文で描写する
- 質問や直訴の場合: その内容に直接答える（NPCの紹介、ルール説明、状況確認など）
- 必ず何かしら内容のある返答を返すこと。空の返答は禁止。`
	if ragCtx != "" {
		systemPrompt += "\n\n【参照ルール】\n" + ragCtx
	}
	msgs := []llm.Message{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: input.Text},
	}
	res, attempts, err := e.runStage(ctx, tr.SessionID, tr.Turn, stage{
		name:      "gm",
		opts:      llm.CompletionOptions{Temperature: 0.8, MaxTokens: 2048},
		retryHint: "GMとして情景・結果を日本語の地の文で描写し直してください。",
		validate:  func(r llm.Result) error { return ValidateGMNarration(r) },
	}, msgs)
	tr.StageAttempts["gm"] = attempts
	return strings.TrimSpace(res.Text), err
}

// runNPCs asks each NPC for its action, one after another. An NPC whose
// output never validates sits out this turn (recorded in tr.Warnings); a
// transport error aborts the turn.
func (e *Engine) runNPCs(ctx context.Context, tr *TurnResult, gmNarration string, sheets []db.NPCSheet) ([]NPCAction, error) {
	var actions []NPCAction
	for _, sheet := range sheets {
		currentLane := positionToLane(sheet.PositionY)
		basePrompt := fmt.Sprintf(
			"あなたはNPC「%s」です。現在HP=%d / MP=%d。現在のレーン: %s。\n"+
				"戦闘は3レーン制（enemy=敵後衛 / front=フロントライン / party=味方後衛）です。\n"+
				"状況を受け、以下のYAML形式のみで行動を出力してください。コードブロック・説明文は一切不要。\n"+
				"制約: mp_costは0以上%d以下の整数、hp_deltaは%d以上0以下の整数。\n"+
				"new_laneは「不意打ちで後衛が前線に引き出された」「前線崩壊で押し込まれた」など"+
				"レーンが実際に変わる場合のみ enemy/front/party のいずれかを記入し、変化なければ省略。\n"+
				"name: %s\naction: <行動>\ndialogue: <台詞>\nmp_cost: <消費MP整数>\nhp_delta: <HP変化整数>\nnew_lane: <省略可>",
			sheet.Name, sheet.HP, sheet.MP, currentLane,
			sheet.MP, -sheet.HP,
			sheet.Name,
		)
		msgs := []llm.Message{
			{Role: "system", Content: basePrompt},
			{Role: "user", Content: gmNarration},
		}

		var action *NPCAction
		_, attempts, err := e.runStage(ctx, tr.SessionID, tr.Turn, stage{
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
		}, msgs)
		tr.StageAttempts["npc:"+sheet.Name] = attempts
		if errors.Is(err, errRejected) {
			tr.Warnings = append(tr.Warnings, fmt.Sprintf("NPC %s は行動できませんでした: %v", sheet.Name, err))
			continue
		}
		if err != nil {
			return actions, fmt.Errorf("NPC %s: %w", sheet.Name, err)
		}
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
func (e *Engine) calculateResources(ctx context.Context, tr *TurnResult, actions []NPCAction, sheets []db.NPCSheet) ([]ResourceDelta, error) {
	if len(actions) == 0 {
		return nil, nil
	}

	actionsJSON, _ := json.Marshal(actions)
	msgs := []llm.Message{
		{Role: "system", Content: `あなたはTRPGの数値計算担当です。
NPC行動リストを受け取り、各NPCのHP変化とMP消費を確定してください。
必ずJSON配列のみを返してください。説明文・コードブロック・改行以外の余分な文字は一切不要です。
形式（厳守）: [{"npc_name":"名前","hp_delta":0,"mp_cost":0}]
行動に攻撃がなければhp_deltaは0、魔法でなければmp_costは0。`},
		{Role: "user", Content: string(actionsJSON)},
	}

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
	}, msgs)
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
