package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/kf/sw25-omni-master/pkg/db"
	"github.com/kf/sw25-omni-master/pkg/game"
	"nhooyr.io/websocket"
	"nhooyr.io/websocket/wsjson"
	"gopkg.in/yaml.v3"
)

// wsClient wraps a WebSocket connection with a serialized write channel.
type wsClient struct {
	conn  *websocket.Conn
	sendC chan any
}

// Handler holds shared state and serves all HTTP/WS routes.
type Handler struct {
	database    *sql.DB
	engine      *game.Engine
	comfy       *game.ComfyUIClient
	personasDir string

	mu      sync.RWMutex
	clients map[*wsClient]struct{}
}

func New(database *sql.DB, engine *game.Engine, comfy *game.ComfyUIClient, personasDir string) *Handler {
	return &Handler{
		database:    database,
		engine:      engine,
		comfy:       comfy,
		personasDir: personasDir,
		clients:     make(map[*wsClient]struct{}),
	}
}

// Routes registers all endpoints on the given mux.
func (h *Handler) Routes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/turn", h.handleTurn)
	mux.HandleFunc("POST /api/gm-channel", h.handleGMChannel)
	mux.HandleFunc("GET /api/session/{id}", h.handleGetSession)
	mux.HandleFunc("GET /api/checkpoint/restore/{turn}", h.handleRestoreCheckpoint)
	mux.HandleFunc("GET /api/comfy/status", h.handleComfyStatus)
	mux.HandleFunc("PATCH /api/npc-position", h.handleNPCPosition)
	mux.HandleFunc("GET /api/personas", h.handleListPersonas)
	mux.HandleFunc("POST /api/npc-sheet", h.handleAddNPCSheet)
	mux.HandleFunc("DELETE /api/npc-sheet", h.handleDeleteNPCSheet)
	mux.HandleFunc("PATCH /api/npc-sheet/{id}", h.handleUpdateNPCSheet)
	mux.HandleFunc("GET /api/quests", h.handleListQuests)
	mux.HandleFunc("PATCH /api/quests/{id}/accept", h.handleAcceptQuest)
	mux.HandleFunc("PATCH /api/quests/{id}/complete", h.handleCompleteQuest)
	mux.HandleFunc("GET /api/player-characters", h.handleListPCs)
	mux.HandleFunc("POST /api/player-characters", h.handleCreatePC)
	mux.HandleFunc("PUT /api/player-characters/{id}", h.handleUpdatePC)
	mux.HandleFunc("DELETE /api/player-characters/{id}", h.handleDeletePC)
	mux.HandleFunc("PATCH /api/player-characters/{id}/activate", h.handleActivatePC)
	mux.HandleFunc("PATCH /api/player-characters/{id}/deactivate", h.handleDeactivatePC)
	mux.HandleFunc("GET /api/rules", h.handleListRules)
	mux.HandleFunc("POST /api/rules", h.handleCreateRule)
	mux.HandleFunc("PATCH /api/rules/{id}", h.handleUpdateRule)
	mux.HandleFunc("DELETE /api/rules/{id}", h.handleDeleteRule)
	// DEV-mode endpoints
	mux.HandleFunc("DELETE /api/checkpoint/{id}", h.handleDeleteCheckpoint)
	mux.HandleFunc("DELETE /api/session-log/turn/{turn}", h.handleDeleteSessionLogTurn)
	// Scenario batch endpoints
	mux.HandleFunc("GET /api/scenarios", h.handleListScenarios)
	mux.HandleFunc("GET /api/scenarios/{id}", h.handleGetScenario)
	mux.HandleFunc("GET /api/scenarios/backgrounds", h.handleListBackgrounds)
	mux.HandleFunc("GET /api/scenarios/{id}/image/{imgid}", h.handleServeScenarioImage)
	mux.HandleFunc("PATCH /api/scenarios/{id}/status", h.handleUpdateScenarioStatus)
	mux.HandleFunc("GET /ws", h.handleWebSocket)
}

func (h *Handler) handleTurn(w http.ResponseWriter, r *http.Request) {
	var input game.PlayerInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
	if input.SessionID == 0 || input.Text == "" {
		http.Error(w, "session_id and text are required", http.StatusBadRequest)
		return
	}

	result, err := h.engine.ProcessTurn(r.Context(), input)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	h.broadcast(result)
	h.saveTurnResult(result)
	go h.generateAndBroadcastImage(result.SessionID, result.Turn, result.GMNarration)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

func (h *Handler) handleGMChannel(w http.ResponseWriter, r *http.Request) {
	var input game.PlayerInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	input.Text = "[GM直訴] " + input.Text

	result, err := h.engine.ProcessTurn(r.Context(), input)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	h.broadcast(result)
	h.saveTurnResult(result)
	go h.generateAndBroadcastImage(result.SessionID, result.Turn, result.GMNarration)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

func (h *Handler) handleGetSession(w http.ResponseWriter, r *http.Request) {
	idStr := strings.TrimPrefix(r.PathValue("id"), "")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "invalid session id", http.StatusBadRequest)
		return
	}

	session, err := db.GetSession(h.database, id)
	if err != nil {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}
	sheets, err := db.GetNPCSheets(h.database, id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	checkpoints, _ := db.ListCheckpoints(h.database, id)
	turnJSONs, _ := db.ListTurnResults(h.database, id)

	// Wrap raw JSON strings into a valid JSON array.
	turnsArray := json.RawMessage("[]")
	if len(turnJSONs) > 0 {
		buf := []byte("[")
		for i, s := range turnJSONs {
			if i > 0 {
				buf = append(buf, ',')
			}
			buf = append(buf, []byte(s)...)
		}
		buf = append(buf, ']')
		turnsArray = buf
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"session":      session,
		"npc_sheets":   sheets,
		"checkpoints":  checkpoints,
		"turn_results": turnsArray,
	})
}

func (h *Handler) handleRestoreCheckpoint(w http.ResponseWriter, r *http.Request) {
	turnStr := r.PathValue("turn")
	turn, err := strconv.Atoi(turnStr)
	if err != nil {
		http.Error(w, "invalid turn", http.StatusBadRequest)
		return
	}
	sessionID, err := strconv.ParseInt(r.URL.Query().Get("session_id"), 10, 64)
	if err != nil {
		http.Error(w, "session_id query param required", http.StatusBadRequest)
		return
	}

	if _, err := db.GetCheckpoint(h.database, sessionID, turn); err != nil {
		http.Error(w, "checkpoint not found", http.StatusNotFound)
		return
	}

	// Truncate logs and results to the restored turn.
	_ = db.DeleteSessionLogsAfter(h.database, sessionID, turn)
	_ = db.DeleteTurnResultsAfter(h.database, sessionID, turn)

	// Return the remaining turn results so the client can rebuild the chat log.
	turnJSONs, err := db.ListTurnResults(h.database, sessionID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	buf := []byte("[")
	for i, s := range turnJSONs {
		if i > 0 {
			buf = append(buf, ',')
		}
		buf = append(buf, []byte(s)...)
	}
	buf = append(buf, ']')

	w.Header().Set("Content-Type", "application/json")
	w.Write(buf)
}

func (h *Handler) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify: true,
	})
	if err != nil {
		return
	}

	client := &wsClient{conn: conn, sendC: make(chan any, 16)}

	h.mu.Lock()
	h.clients[client] = struct{}{}
	h.mu.Unlock()

	defer func() {
		h.mu.Lock()
		delete(h.clients, client)
		h.mu.Unlock()
		conn.CloseNow()
	}()

	// Dedicated write goroutine — serializes all writes to this connection.
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	go func() {
		for {
			select {
			case msg, ok := <-client.sendC:
				if !ok {
					return
				}
				_ = wsjson.Write(ctx, conn, msg)
			case <-ctx.Done():
				return
			}
		}
	}()

	// Read loop: discard frames, exit on close.
	for {
		if _, _, err := conn.Read(ctx); err != nil {
			return
		}
	}
}

func (h *Handler) saveTurnResult(result *game.TurnResult) {
	b, err := json.Marshal(result)
	if err != nil {
		log.Printf("[turn] marshal failed: %v", err)
		return
	}
	if err := db.SaveTurnResult(h.database, result.SessionID, result.Turn, string(b)); err != nil {
		log.Printf("[turn] SaveTurnResult failed: %v", err)
	}
}

func (h *Handler) broadcast(v any) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for client := range h.clients {
		select {
		case client.sendC <- v:
		default: // drop if buffer full
		}
	}
}

// generateAndBroadcastImage queues an image job and broadcasts ImageUpdate when ready.
func (h *Handler) generateAndBroadcastImage(sessionID int64, turn int, narration string) {
	if h.comfy == nil {
		return
	}
	prompt := buildImagePrompt(narration)
	promptID, err := h.comfy.RequestImage(context.Background(), prompt)
	if err != nil {
		log.Printf("[comfy] turn %d request failed: %v", turn, err)
		return
	}
	imageURL, err := h.comfy.PollImage(context.Background(), promptID)
	if err != nil {
		log.Printf("[comfy] turn %d poll failed: %v", turn, err)
		return
	}
	h.broadcast(game.ImageUpdate{SessionID: sessionID, Turn: turn, ImageURL: imageURL})
}

// handleComfyStatus reports whether ComfyUI is reachable.
func (h *Handler) handleComfyStatus(w http.ResponseWriter, r *http.Request) {
	type statusResp struct {
		Available bool   `json:"available"`
		URL       string `json:"url"`
	}
	var resp statusResp
	if h.comfy != nil {
		resp.Available = h.comfy.Ping(r.Context())
		if resp.Available {
			resp.URL = h.comfy.BaseURL()
		}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// handleNPCPosition updates a single NPC's lane for manual formation edits.
// Body: {"session_id": 1, "name": "ガルド", "lane": "front"}
func (h *Handler) handleNPCPosition(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SessionID int64  `json:"session_id"`
		Name      string `json:"name"`
		Lane      string `json:"lane"` // "enemy" | "front" | "party"
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}

	laneY := map[string]int{"front": 4, "party": 7}
	newY, ok := laneY[req.Lane]
	if !ok {
		http.Error(w, "lane must be front|party", http.StatusBadRequest)
		return
	}

	sheets, err := db.GetNPCSheets(h.database, req.SessionID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	var target *db.NPCSheet
	for i := range sheets {
		if sheets[i].Name == req.Name {
			target = &sheets[i]
			break
		}
	}
	if target == nil {
		http.Error(w, "npc not found", http.StatusNotFound)
		return
	}
	target.PositionY = newY
	if err := db.UpsertNPCSheet(h.database, *target); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// personaInfo is the minimal YAML fields we read for the persona list.
type personaInfo struct {
	ID   string `yaml:"id"   json:"id"`
	Name string `yaml:"name" json:"name"`
	HP   int    `yaml:"hp"   json:"hp"`
	MP   int    `yaml:"mp"   json:"mp"`
}

// handleListPersonas returns all persona YAML files found in personasDir.
func (h *Handler) handleListPersonas(w http.ResponseWriter, r *http.Request) {
	entries, err := os.ReadDir(h.personasDir)
	if err != nil {
		http.Error(w, "cannot read personas dir: "+err.Error(), http.StatusInternalServerError)
		return
	}
	var personas []personaInfo
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(h.personasDir, e.Name()))
		if err != nil {
			continue
		}
		var p personaInfo
		if err := yaml.Unmarshal(raw, &p); err != nil || p.Name == "" {
			continue
		}
		personas = append(personas, p)
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(personas)
}

// handleAddNPCSheet loads a persona YAML and upserts it into npc_sheets.
// Body: {"session_id": 1, "persona_id": "gard"}
func (h *Handler) handleAddNPCSheet(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SessionID int64  `json:"session_id"`
		PersonaID string `json:"persona_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}

	// Find the YAML file whose id field matches
	entries, _ := os.ReadDir(h.personasDir)
	var raw []byte
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(h.personasDir, e.Name()))
		if err != nil {
			continue
		}
		var p personaInfo
		if yaml.Unmarshal(b, &p) == nil && p.ID == req.PersonaID {
			raw = b
			break
		}
	}
	if raw == nil {
		http.Error(w, "persona not found", http.StatusNotFound)
		return
	}

	var p personaInfo
	_ = yaml.Unmarshal(raw, &p)
	sheet := db.NPCSheet{
		SessionID: req.SessionID,
		Name:      p.Name,
		HP:        p.HP,
		MP:        p.MP,
		PositionX: 0,
		PositionY: 4, // default: front line
		YAMLBlob:  string(raw),
	}
	if err := db.UpsertNPCSheet(h.database, sheet); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleDeleteNPCSheet removes an NPC from npc_sheets.
// Body: {"session_id": 1, "name": "ガルド"}
func (h *Handler) handleDeleteNPCSheet(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SessionID int64  `json:"session_id"`
		Name      string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	if err := db.DeleteNPCSheet(h.database, req.SessionID, req.Name); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleUpdateNPCSheet updates HP, MP and YAML blob for a single NPC sheet.
// Body: {"hp": 34, "mp": 8, "yaml_blob": "..."}
func (h *Handler) handleUpdateNPCSheet(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	var req struct {
		HP       int    `json:"hp"`
		MP       int    `json:"mp"`
		YAMLBlob string `json:"yaml_blob"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	if _, err := h.database.Exec(
		`UPDATE npc_sheets SET hp=?, mp=?, yaml_blob=?, updated_at=CURRENT_TIMESTAMP WHERE id=?`,
		req.HP, req.MP, req.YAMLBlob, id,
	); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) handleListQuests(w http.ResponseWriter, r *http.Request) {
	quests, err := db.ListQuests(h.database)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if quests == nil {
		quests = []db.Quest{}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(quests)
}

func (h *Handler) handleAcceptQuest(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	if err := db.AcceptQuest(h.database, id); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) handleCompleteQuest(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	if err := db.CompleteQuest(h.database, id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) handleListPCs(w http.ResponseWriter, r *http.Request) {
	pcs, err := db.ListPlayerCharacters(h.database)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	type pcResp struct {
		ID       int64  `json:"id"`
		Name     string `json:"name"`
		JSONBlob string `json:"json_blob"`
		IsActive bool   `json:"is_active"`
	}
	out := make([]pcResp, 0, len(pcs))
	for _, pc := range pcs {
		out = append(out, pcResp{pc.ID, pc.Name, pc.JSONBlob, pc.IsActive})
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(out)
}

func (h *Handler) handleCreatePC(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name     string `json:"name"`
		JSONBlob string `json:"json_blob"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	if req.Name == "" {
		req.Name = "PC"
	}
	pc, err := db.CreatePlayerCharacter(h.database, req.Name, req.JSONBlob)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]any{"id": pc.ID, "name": pc.Name})
}

func (h *Handler) handleUpdatePC(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	var req struct {
		Name     string `json:"name"`
		JSONBlob string `json:"json_blob"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	if err := db.UpdatePlayerCharacter(h.database, id, req.Name, req.JSONBlob); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) handleDeletePC(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	if err := db.DeletePlayerCharacter(h.database, id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) handleActivatePC(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	if err := db.ActivatePlayerCharacter(h.database, id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) handleDeactivatePC(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	if err := db.DeactivatePlayerCharacter(h.database, id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ── Rules (rag_chunks) CRUD ──────────────────────────────────────────────────

// ruleJSON is the JSON shape returned to and accepted from the UI.
type ruleJSON struct {
	ID          int64   `json:"id"`
	SourceType  string  `json:"source_type"`
	Priority    int     `json:"priority"`
	Tag         string  `json:"tag"`
	Text        string  `json:"text"`
	Enabled     bool    `json:"enabled"`
	OverridesID *int64  `json:"overrides_id"`
}

func (h *Handler) handleListRules(w http.ResponseWriter, r *http.Request) {
	sourceType := r.URL.Query().Get("source_type")
	chunks, err := db.ListRAGChunks(h.database, sourceType)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	out := make([]ruleJSON, 0, len(chunks))
	for _, c := range chunks {
		out = append(out, ruleJSON{
			ID: c.ID, SourceType: c.SourceType, Priority: c.Priority,
			Tag: c.Tag, Text: c.Text, Enabled: c.Enabled, OverridesID: c.OverridesID,
		})
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(out)
}

func (h *Handler) handleCreateRule(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SourceType  string `json:"source_type"`
		Tag         string `json:"tag"`
		Text        string `json:"text"`
		OverridesID *int64 `json:"overrides_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Text == "" {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	// Only houserule and correction can be created via API
	priorityMap := map[string]int{"houserule": 20, "correction": 10}
	priority, ok := priorityMap[req.SourceType]
	if !ok {
		http.Error(w, "source_type must be houserule or correction", http.StatusBadRequest)
		return
	}
	id, err := db.InsertRAGChunk(h.database, db.RAGChunk{
		SourceType:  req.SourceType,
		Priority:    priority,
		Tag:         req.Tag,
		Text:        req.Text,
		Enabled:     true,
		OverridesID: req.OverridesID,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(ruleJSON{
		ID: id, SourceType: req.SourceType, Priority: priority,
		Tag: req.Tag, Text: req.Text, Enabled: true, OverridesID: req.OverridesID,
	})
}

func (h *Handler) handleUpdateRule(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	var req struct {
		Text    string `json:"text"`
		Tag     string `json:"tag"`
		Enabled bool   `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	if err := db.UpdateRAGChunk(h.database, id, req.Text, req.Tag, req.Enabled); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) handleDeleteRule(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	if err := db.DeleteRAGChunk(h.database, id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleDeleteCheckpoint deletes a single checkpoint by ID (DEV mode).
func (h *Handler) handleDeleteCheckpoint(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	if _, err := h.database.Exec(`DELETE FROM checkpoints WHERE id = ?`, id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleDeleteSessionLogTurn deletes all session_logs for a given turn (DEV mode).
// Query param session_id is required.
func (h *Handler) handleDeleteSessionLogTurn(w http.ResponseWriter, r *http.Request) {
	turn, err := strconv.Atoi(r.PathValue("turn"))
	if err != nil {
		http.Error(w, "invalid turn", http.StatusBadRequest)
		return
	}
	sessionID, err := strconv.ParseInt(r.URL.Query().Get("session_id"), 10, 64)
	if err != nil {
		http.Error(w, "session_id query param required", http.StatusBadRequest)
		return
	}
	if _, err := h.database.Exec(
		`DELETE FROM session_logs WHERE session_id = ? AND turn = ?`, sessionID, turn,
	); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ── Scenarios ────────────────────────────────────────────────────────────────

func (h *Handler) handleListScenarios(w http.ResponseWriter, r *http.Request) {
	status := r.URL.Query().Get("status") // optional filter: draft/ready/used
	scenarios, err := db.ListScenarios(h.database, status)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	type scenarioResp struct {
		ID          int64  `json:"id"`
		Title       string `json:"title"`
		Rank        string `json:"rank"`
		Summary     string `json:"summary"`
		Client      string `json:"client"`
		Reward      string `json:"reward"`
		Target      string `json:"target"`
		Level       string `json:"level"`
		Status      string `json:"status"`
		GeneratedAt string `json:"generated_at"`
	}
	out := make([]scenarioResp, 0, len(scenarios))
	for _, s := range scenarios {
		out = append(out, scenarioResp{s.ID, s.Title, s.Rank, s.Summary,
			s.Client, s.Reward, s.Target, s.Level, s.Status, s.GeneratedAt})
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(out)
}

func (h *Handler) handleGetScenario(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	s, err := db.GetScenario(h.database, id)
	if err != nil {
		http.Error(w, "scenario not found", http.StatusNotFound)
		return
	}
	images, _ := db.ListScenarioImages(h.database, id)

	type imgResp struct {
		ID       int64  `json:"id"`
		Category string `json:"category"`
		Label    string `json:"label"`
		FilePath string `json:"file_path"`
		Width    int    `json:"width"`
		Height   int    `json:"height"`
	}
	imgOut := make([]imgResp, 0, len(images))
	for _, img := range images {
		imgOut = append(imgOut, imgResp{img.ID, img.Category, img.Label, img.FilePath, img.Width, img.Height})
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"id": s.ID, "title": s.Title, "rank": s.Rank, "summary": s.Summary,
		"description": s.Description, "client": s.Client, "reward": s.Reward,
		"target": s.Target, "level": s.Level, "status": s.Status,
		"generated_at":    s.GeneratedAt,
		"locations_json":  s.LocationsJSON,
		"enemies_json":    s.EnemiesJSON,
		"plot_hooks_json": s.PlotHooksJSON,
		"events_json":     s.EventsJSON,
		"images":          imgOut,
	})
}

func (h *Handler) handleListBackgrounds(w http.ResponseWriter, r *http.Request) {
	images, err := db.ListBackgroundImages(h.database)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	type imgResp struct {
		ID       int64  `json:"id"`
		Category string `json:"category"`
		Label    string `json:"label"`
		FilePath string `json:"file_path"`
	}
	out := make([]imgResp, 0, len(images))
	for _, img := range images {
		out = append(out, imgResp{img.ID, img.Category, img.Label, img.FilePath})
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(out)
}

// handleServeScenarioImage serves the image file by reading from the local filesystem.
// This avoids having to serve static files separately.
func (h *Handler) handleServeScenarioImage(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	imgID, err := strconv.ParseInt(r.PathValue("imgid"), 10, 64)
	if err != nil {
		http.Error(w, "invalid imgid", http.StatusBadRequest)
		return
	}
	row := h.database.QueryRow(
		`SELECT file_path FROM scenario_images WHERE id=? AND scenario_id=?`, imgID, id)
	var filePath string
	if err := row.Scan(&filePath); err != nil {
		http.Error(w, "image not found", http.StatusNotFound)
		return
	}
	http.ServeFile(w, r, filePath)
}

func (h *Handler) handleUpdateScenarioStatus(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	var req struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	valid := map[string]bool{"draft": true, "ready": true, "used": true}
	if !valid[req.Status] {
		http.Error(w, "status must be draft|ready|used", http.StatusBadRequest)
		return
	}
	if err := db.UpdateScenarioStatus(h.database, id, req.Status); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func buildImagePrompt(narration string) string {
	if len(narration) > 200 {
		narration = narration[:200]
	}
	return "fantasy TRPG scene, " + narration
}
