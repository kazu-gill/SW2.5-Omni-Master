package game

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/kf/sw25-omni-master/pkg/db"
	"github.com/kf/sw25-omni-master/pkg/llm"
	"github.com/kf/sw25-omni-master/pkg/rag"
)

const npcRetryLimit = 2

// LLMPool holds the llama-server clients for each agent role.
type LLMPool struct {
	GM      *llm.Client
	NPCs    [3]*llm.Client // ports 11432-11434
	Support *llm.Client
}

// Engine orchestrates a full TRPG turn.
type Engine struct {
	db   *sql.DB
	pool LLMPool
	rag  *rag.Engine
}

func NewEngine(database *sql.DB, pool LLMPool, ragEngine *rag.Engine) *Engine {
	return &Engine{db: database, pool: pool, rag: ragEngine}
}

// ProcessTurn executes one full game turn:
//  1. Save checkpoint
//  2. GM narration (with RAG context)
//  3. Parallel NPC action broadcast
//  4. Support LM resource calculation
//  5. DB update
func (e *Engine) ProcessTurn(ctx context.Context, input PlayerInput) (*TurnResult, error) {
	// 1. Checkpoint
	if err := e.saveCheckpoint(ctx, input.SessionID, input.Turn); err != nil {
		return nil, fmt.Errorf("checkpoint: %w", err)
	}

	// Log player input
	_ = db.AppendLog(e.db, input.SessionID, input.Turn, "player", input.Text)

	// 2. GM narration
	ragChunks, err := e.rag.Search(ctx, input.Text, 5)
	if err != nil {
		ragChunks = nil // RAG failure is non-fatal; continue without context
	}
	ragCtx := e.rag.BuildContext(ragChunks)
	gmNarration, err := e.gmNarrate(ctx, input, ragCtx)
	if err != nil {
		return nil, fmt.Errorf("gm narration: %w", err)
	}
	_ = db.AppendLog(e.db, input.SessionID, input.Turn, "gm", gmNarration)

	// 3. Load NPC sheets and broadcast in parallel
	sheets, err := db.GetNPCSheets(e.db, input.SessionID)
	if err != nil {
		return nil, fmt.Errorf("get npc sheets: %w", err)
	}
	npcActions, err := e.broadcastNPCs(ctx, input.SessionID, input.Turn, gmNarration, sheets)
	if err != nil {
		return nil, fmt.Errorf("npc broadcast: %w", err)
	}

	// 5. Support LM resource calculation
	deltas, err := e.calculateResources(ctx, input.SessionID, input.Turn, npcActions, sheets)
	if err != nil {
		return nil, fmt.Errorf("resource calc: %w", err)
	}

	// Apply deltas to DB
	if err := e.applyDeltas(deltas, sheets); err != nil {
		return nil, fmt.Errorf("apply deltas: %w", err)
	}

	// Apply formation changes from NPC actions
	formationChanges := e.applyFormationChanges(npcActions, sheets)

	return &TurnResult{
		SessionID:        input.SessionID,
		Turn:             input.Turn,
		GMNarration:      gmNarration,
		NPCActions:       npcActions,
		Deltas:           deltas,
		FormationChanges: formationChanges,
	}, nil
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

func (e *Engine) gmNarrate(ctx context.Context, input PlayerInput, ragCtx string) (string, error) {
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
	return e.pool.GM.Complete(ctx, msgs, llm.CompletionOptions{Temperature: 0.8, MaxTokens: 2048})
}

func (e *Engine) broadcastNPCs(ctx context.Context, sessionID int64, turn int, gmNarration string, sheets []db.NPCSheet) ([]NPCAction, error) {
	if len(sheets) == 0 {
		return nil, nil
	}

	type npcResult struct {
		action *NPCAction
		err    error
	}
	results := make([]npcResult, len(sheets))

	var wg sync.WaitGroup
	for i, s := range sheets {
		wg.Add(1)
		go func(idx int, sheet db.NPCSheet) {
			defer wg.Done()
			client := e.pool.NPCs[idx%3]
			opts := llm.CompletionOptions{Temperature: 0.7, MaxTokens: 2048}

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
			var validErr error
			var yamlText string

			for attempt := 0; attempt <= npcRetryLimit; attempt++ {
				text, err := client.Complete(ctx, msgs, opts)
				if err != nil {
					validErr = err
					break
				}
				yamlText = text
				cleaned := stripCodeBlock(text)
				action, validErr = ParseNPCAction(cleaned)
				if validErr == nil {
					validErr = ValidateNPCAction(action, sheet.HP, sheet.MP)
				}
				if validErr == nil {
					break
				}
				if attempt < npcRetryLimit {
					msgs = append(msgs,
						llm.Message{Role: "assistant", Content: text},
						llm.Message{Role: "user", Content: fmt.Sprintf(
							"エラー: %v\nmp_costは0〜%d、hp_deltaは%d〜0の範囲でYAMLのみ再出力してください。",
							validErr, sheet.MP, -sheet.HP,
						)},
					)
				}
			}

			_ = db.AppendLog(e.db, sessionID, turn, "npc", yamlText)
			results[idx] = npcResult{action, validErr}
		}(i, s)
	}
	wg.Wait()

	var actions []NPCAction
	var errs []string
	for i, res := range results {
		if res.err != nil {
			errs = append(errs, fmt.Sprintf("NPC[%d]: %v", i, res.err))
			continue
		}
		if res.action != nil {
			actions = append(actions, *res.action)
		}
	}
	if len(errs) > 0 {
		return actions, fmt.Errorf("some NPCs failed: %s", strings.Join(errs, "; "))
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

func (e *Engine) calculateResources(ctx context.Context, sessionID int64, turn int, actions []NPCAction, sheets []db.NPCSheet) ([]ResourceDelta, error) {
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
	raw, err := e.pool.Support.Complete(ctx, msgs, llm.CompletionOptions{MaxTokens: 512})
	if err != nil {
		return nil, err
	}
	_ = db.AppendLog(e.db, sessionID, turn, "support", raw)

	var deltas []ResourceDelta
	if err := json.Unmarshal([]byte(raw), &deltas); err != nil {
		// Fallback: use values directly from NPCAction
		for _, a := range actions {
			deltas = append(deltas, ResourceDelta{NPCName: a.Name, HPDelta: a.HPDelta, MPCost: a.MPCost})
		}
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

