package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/kf/sw25-omni-master/pkg/db"
	"github.com/kf/sw25-omni-master/pkg/game"
	"github.com/kf/sw25-omni-master/pkg/images"
	"github.com/kf/sw25-omni-master/pkg/rag"
	"gopkg.in/yaml.v3"
	"nhooyr.io/websocket"
	"nhooyr.io/websocket/wsjson"
)

// wsClient wraps a WebSocket connection with a serialized write channel.
// sessionID filters what it receives (0 = every session).
type wsClient struct {
	conn      *websocket.Conn
	sendC     chan any
	sessionID int64
}

// Endpoint is an mlx-serve profile the orchestrator uses.
type Endpoint struct {
	URL     string `json:"url"`
	ModelID string `json:"model_id"`
}

// Options configures a Handler.
type Options struct {
	PersonasDir string
	TurnTimeout time.Duration
	Images      *images.Client // nil: no scene images
	ImageEvery  int
	ImagesDir   string
	LLM, Embed  Endpoint
	APCTenant   string
}

// Handler holds shared state and serves all HTTP/WS routes.
type Handler struct {
	database    *sql.DB
	engine      *game.Engine
	rag         *rag.Engine // embeds rules added or edited from the UI
	personasDir string
	turns       *turnRunner
	turnTimeout time.Duration
	images      *images.Client
	imageEvery  int
	imagesDir   string
	llmEP       Endpoint
	embedEP     Endpoint
	apcTenant   string

	mu      sync.RWMutex
	clients map[*wsClient]struct{}
}

func New(database *sql.DB, engine *game.Engine, ragEngine *rag.Engine, opts Options) *Handler {
	if opts.TurnTimeout == 0 {
		opts.TurnTimeout = 15 * time.Minute
	}
	if opts.ImageEvery == 0 {
		opts.Images = nil
	}
	return &Handler{
		database:    database,
		engine:      engine,
		rag:         ragEngine,
		personasDir: opts.PersonasDir,
		turns:       newTurnRunner(),
		turnTimeout: opts.TurnTimeout,
		images:      opts.Images,
		imageEvery:  opts.ImageEvery,
		imagesDir:   opts.ImagesDir,
		llmEP:       opts.LLM,
		embedEP:     opts.Embed,
		apcTenant:   opts.APCTenant,
		clients:     make(map[*wsClient]struct{}),
	}
}

// Routes registers all endpoints on the given mux.
func (h *Handler) Routes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/system", h.handleSystem)
	// Sessions and turns
	mux.HandleFunc("GET /api/sessions", h.handleListSessions)
	mux.HandleFunc("POST /api/sessions", h.handleCreateSession)
	mux.HandleFunc("GET /api/sessions/{id}", h.handleGetSession)
	mux.HandleFunc("DELETE /api/sessions/{id}", h.handleDeleteSession)
	mux.HandleFunc("POST /api/sessions/{id}/turn", h.handleSessionTurn)
	mux.HandleFunc("POST /api/sessions/{id}/cancel", h.handleCancelTurn)
	mux.HandleFunc("POST /api/sessions/{id}/resume", h.handleResume)
	mux.HandleFunc("POST /api/sessions/{id}/discard", h.handleDiscard)
	mux.HandleFunc("POST /api/sessions/{id}/redo", h.handleRedo)
	mux.HandleFunc("POST /api/sessions/{id}/restore", h.handleRestore)
	mux.HandleFunc("POST /api/sessions/{id}/quest", h.handleSessionQuest)
	mux.HandleFunc("GET /api/sessions/{id}/images/{file}", h.handleSessionImage)
	// Party
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
	// Rules
	mux.HandleFunc("GET /api/rules", h.handleListRules)
	mux.HandleFunc("GET /api/rules/search", h.handleSearchRules)
	mux.HandleFunc("POST /api/rules", h.handleCreateRule)
	mux.HandleFunc("PATCH /api/rules/{id}", h.handleUpdateRule)
	mux.HandleFunc("DELETE /api/rules/{id}", h.handleDeleteRule)
	// DEV-mode endpoints
	mux.HandleFunc("DELETE /api/checkpoint/{id}", h.handleDeleteCheckpoint)
	// Scenario batch endpoints
	mux.HandleFunc("GET /api/scenarios", h.handleListScenarios)
	mux.HandleFunc("GET /api/scenarios/{id}", h.handleGetScenario)
	mux.HandleFunc("GET /api/scenarios/backgrounds", h.handleListBackgrounds)
	mux.HandleFunc("GET /api/scenarios/{id}/image/{imgid}", h.handleServeScenarioImage)
	mux.HandleFunc("PATCH /api/scenarios/{id}/status", h.handleUpdateScenarioStatus)
	mux.HandleFunc("GET /ws", h.handleWebSocket)
}

func (h *Handler) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify: true,
	})
	if err != nil {
		return
	}

	sessionID, _ := strconv.ParseInt(r.URL.Query().Get("session_id"), 10, 64)
	// Streaming sends many small deltas; the buffer covers a slow reader.
	client := &wsClient{conn: conn, sendC: make(chan any, 1024), sessionID: sessionID}

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

	// Tell a new tab whether a turn is already running.
	client.sendC <- map[string]any{"type": "busy", "session_id": sessionID, "busy": h.turns.busy(sessionID)}

	// Read loop: discard frames, exit on close.
	for {
		if _, _, err := conn.Read(ctx); err != nil {
			return
		}
	}
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
		writeError(w, http.StatusBadRequest, "リクエストの形式が正しくありません")
		return
	}

	newY, ok := laneY[req.Lane]
	if !ok {
		writeError(w, http.StatusBadRequest, "lane は enemy / front / party のいずれかです")
		return
	}
	if h.turns.busy(req.SessionID) {
		writeError(w, http.StatusConflict, errBusy.Error())
		return
	}
	if h.refuseIfStopped(w, req.SessionID) {
		return
	}

	sheets, err := db.GetNPCSheets(h.database, req.SessionID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
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
		writeError(w, http.StatusNotFound, "その NPC はこのセッションにいません。画面を再読み込みしてください。")
		return
	}
	target.PositionY = newY
	if err := db.UpsertNPCSheet(h.database, *target); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// personaInfo is the minimal YAML fields we read for the persona list.
type personaInfo struct {
	ID    string `yaml:"id"     json:"id"`
	Name  string `yaml:"name"   json:"name"`
	HP    int    `yaml:"hp"     json:"hp"`
	MP    int    `yaml:"mp"     json:"mp"`
	HPMax int    `yaml:"hp_max" json:"hp_max"`
	MPMax int    `yaml:"mp_max" json:"mp_max"`
}

// maxNPCs is the party size the concept promises: up to 3 NPC companions.
const maxNPCs = 3

// handleListPersonas returns all persona YAML files found in personasDir.
func (h *Handler) handleListPersonas(w http.ResponseWriter, r *http.Request) {
	entries, err := os.ReadDir(h.personasDir)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "ペルソナのフォルダを読めません (config.env の PERSONAS_DIR を確認してください): "+err.Error())
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
		writeError(w, http.StatusBadRequest, "リクエストの形式が正しくありません")
		return
	}
	if h.refuseIfStopped(w, req.SessionID) {
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
		writeError(w, http.StatusNotFound, "ペルソナが見つかりません。data/personas に YAML があるか確認してください。")
		return
	}

	var p personaInfo
	_ = yaml.Unmarshal(raw, &p)
	existing, _ := db.GetNPCSheets(h.database, req.SessionID)
	if len(existing) >= maxNPCs {
		writeError(w, http.StatusBadRequest, "NPC は 3 体までです。入れ替えるときは先に誰かを外してください。")
		return
	}
	if p.HPMax == 0 {
		p.HPMax = p.HP
	}
	if p.MPMax == 0 {
		p.MPMax = p.MP
	}
	sheet := db.NPCSheet{
		SessionID: req.SessionID,
		Name:      p.Name,
		HP:        p.HP,
		MP:        p.MP,
		HPMax:     p.HPMax,
		MPMax:     p.MPMax,
		PositionX: 0,
		PositionY: 4, // default: front line
		YAMLBlob:  string(raw),
	}
	if err := db.UpsertNPCSheet(h.database, sheet); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
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
		writeError(w, http.StatusBadRequest, "リクエストの形式が正しくありません")
		return
	}
	if h.refuseIfStopped(w, req.SessionID) {
		return
	}
	if err := db.DeleteNPCSheet(h.database, req.SessionID, req.Name); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleUpdateNPCSheet updates HP, MP and YAML blob for a single NPC sheet.
// Body: {"hp": 34, "mp": 8, "yaml_blob": "..."}
func (h *Handler) handleUpdateNPCSheet(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "ID が正しくありません")
		return
	}
	var req struct {
		HP       int     `json:"hp"`
		MP       int     `json:"mp"`
		YAMLBlob *string `json:"yaml_blob"` // 省略すると YAML はそのまま
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "リクエストの形式が正しくありません")
		return
	}
	sessionID, err := db.NPCSheetSessionID(h.database, id)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		writeError(w, http.StatusNotFound, "NPC シートが見つかりません。画面を再読み込みしてください。")
		return
	case err != nil:
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if h.refuseIfStopped(w, sessionID) {
		return
	}
	if req.YAMLBlob != nil {
		var probe map[string]any
		if err := yaml.Unmarshal([]byte(*req.YAMLBlob), &probe); err != nil {
			writeError(w, http.StatusBadRequest, "YAML として読めません: "+err.Error())
			return
		}
	}
	if _, err := h.database.Exec(
		`UPDATE npc_sheets SET hp=?, mp=?, yaml_blob=COALESCE(?, yaml_blob), updated_at=CURRENT_TIMESTAMP WHERE id=?`,
		req.HP, req.MP, req.YAMLBlob, id,
	); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) handleListQuests(w http.ResponseWriter, r *http.Request) {
	quests, err := db.ListQuests(h.database)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
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
		writeError(w, http.StatusBadRequest, "ID が正しくありません")
		return
	}
	if h.refuseIfAnyStopped(w) {
		return
	}
	if err := db.AcceptQuest(h.database, id); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) handleCompleteQuest(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "ID が正しくありません")
		return
	}
	if h.refuseIfAnyStopped(w) {
		return
	}
	if err := db.CompleteQuest(h.database, id); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) handleListPCs(w http.ResponseWriter, r *http.Request) {
	pcs, err := db.ListPlayerCharacters(h.database)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
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
		writeError(w, http.StatusBadRequest, "リクエストの形式が正しくありません")
		return
	}
	if req.Name == "" {
		req.Name = "PC"
	}
	pc, err := db.CreatePlayerCharacter(h.database, req.Name, req.JSONBlob)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]any{"id": pc.ID, "name": pc.Name})
}

func (h *Handler) handleUpdatePC(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "ID が正しくありません")
		return
	}
	if h.refuseIfAnyStopped(w) {
		return
	}
	var req struct {
		Name     string `json:"name"`
		JSONBlob string `json:"json_blob"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "リクエストの形式が正しくありません")
		return
	}
	if err := db.UpdatePlayerCharacter(h.database, id, req.Name, req.JSONBlob); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) handleDeletePC(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "ID が正しくありません")
		return
	}
	if h.refuseIfAnyStopped(w) {
		return
	}
	if err := db.DeletePlayerCharacter(h.database, id); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) handleActivatePC(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "ID が正しくありません")
		return
	}
	if h.refuseIfAnyStopped(w) {
		return
	}
	if err := db.ActivatePlayerCharacter(h.database, id); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) handleDeactivatePC(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "ID が正しくありません")
		return
	}
	if h.refuseIfAnyStopped(w) {
		return
	}
	if err := db.DeactivatePlayerCharacter(h.database, id); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ── Rules (rag_chunks) CRUD ──────────────────────────────────────────────────

// ruleJSON is the JSON shape returned to and accepted from the UI.
type ruleJSON struct {
	ID          int64  `json:"id"`
	SourceType  string `json:"source_type"`
	Priority    int    `json:"priority"`
	Tag         string `json:"tag"`
	Text        string `json:"text"`
	Enabled     bool   `json:"enabled"`
	OverridesID *int64 `json:"overrides_id"`
	Section     string `json:"section,omitempty"`
	PageFrom    int    `json:"page_from,omitempty"`
	PageTo      int    `json:"page_to,omitempty"`
	// Embedded reports whether a created rule got its embedding (RAG can find it).
	Embedded *bool `json:"embedded,omitempty"`
}

// embedRule (re)generates a rule's embedding. A failure is logged and
// reported, not fatal: the rule is saved and can be re-embedded later with
// ai-agents/embed_chunks.py.
func (h *Handler) embedRule(ctx context.Context, id int64) bool {
	if h.rag == nil {
		return false
	}
	if err := h.rag.Reembed(ctx, id); err != nil {
		log.Printf("warn: rule %d saved without embedding: %v", id, err)
		return false
	}
	return true
}

// handleListRules pages through the rules. q matches the text or the
// section (部 > 章 > 見出し); source_type narrows to one kind.
func (h *Handler) handleListRules(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	chunks, err := db.ListRAGChunks(h.database, q.Get("source_type"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	limit, offset := 50, 0
	if n, err := strconv.Atoi(q.Get("limit")); err == nil && n > 0 && n <= 500 {
		limit = n
	}
	if n, err := strconv.Atoi(q.Get("offset")); err == nil && n >= 0 {
		offset = n
	}
	needle := strings.TrimSpace(q.Get("q"))
	items := make([]ruleJSON, 0, limit)
	total := 0
	for _, c := range chunks {
		if needle != "" && !strings.Contains(c.Text, needle) && !strings.Contains(c.Section, needle) {
			continue
		}
		total++
		if total <= offset || len(items) == limit {
			continue
		}
		items = append(items, ruleJSON{
			ID: c.ID, SourceType: c.SourceType, Priority: c.Priority,
			Tag: c.Tag, Text: c.Text, Enabled: c.Enabled, OverridesID: c.OverridesID,
			Section: c.Section, PageFrom: c.PageFrom, PageTo: c.PageTo,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"total": total, "items": items})
}

func (h *Handler) handleCreateRule(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SourceType  string `json:"source_type"`
		Tag         string `json:"tag"`
		Text        string `json:"text"`
		OverridesID *int64 `json:"overrides_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Text == "" {
		writeError(w, http.StatusBadRequest, "本文を入力してください")
		return
	}
	// Only houserule and correction can be created via API
	priorityMap := map[string]int{"houserule": 20, "correction": 10}
	priority, ok := priorityMap[req.SourceType]
	if !ok {
		writeError(w, http.StatusBadRequest, "追加できるのはハウスルールか訂正だけです")
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
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	embedded := h.embedRule(r.Context(), id)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(ruleJSON{
		ID: id, SourceType: req.SourceType, Priority: priority,
		Tag: req.Tag, Text: req.Text, Enabled: true, OverridesID: req.OverridesID,
		Embedded: &embedded,
	})
}

func (h *Handler) handleUpdateRule(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "ID が正しくありません")
		return
	}
	var req struct {
		Text    string `json:"text"`
		Tag     string `json:"tag"`
		Enabled bool   `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "リクエストの形式が正しくありません")
		return
	}
	if err := db.UpdateRAGChunk(h.database, id, req.Text, req.Tag, req.Enabled); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.embedRule(r.Context(), id) // the text may have changed
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) handleDeleteRule(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "ID が正しくありません")
		return
	}
	if h.rag != nil {
		defer h.rag.InvalidateTerms()
	}
	if err := db.DeleteRAGChunk(h.database, id); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleDeleteCheckpoint deletes a single checkpoint by ID (DEV mode).
func (h *Handler) handleDeleteCheckpoint(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "ID が正しくありません")
		return
	}
	if _, err := h.database.Exec(`DELETE FROM checkpoints WHERE id = ?`, id); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ── Scenarios ────────────────────────────────────────────────────────────────

func (h *Handler) handleListScenarios(w http.ResponseWriter, r *http.Request) {
	status := r.URL.Query().Get("status") // optional filter: draft/ready/used
	scenarios, err := db.ListScenarios(h.database, status)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
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
		writeError(w, http.StatusBadRequest, "ID が正しくありません")
		return
	}
	s, err := db.GetScenario(h.database, id)
	if err != nil {
		writeError(w, http.StatusNotFound, "シナリオが見つかりません")
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
		writeError(w, http.StatusInternalServerError, err.Error())
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
		writeError(w, http.StatusBadRequest, "ID が正しくありません")
		return
	}
	imgID, err := strconv.ParseInt(r.PathValue("imgid"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "画像 ID が正しくありません")
		return
	}
	row := h.database.QueryRow(
		`SELECT file_path FROM scenario_images WHERE id=? AND scenario_id=?`, imgID, id)
	var filePath string
	if err := row.Scan(&filePath); err != nil {
		writeError(w, http.StatusNotFound, "画像が見つかりません")
		return
	}
	http.ServeFile(w, r, filePath)
}

func (h *Handler) handleUpdateScenarioStatus(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "ID が正しくありません")
		return
	}
	var req struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "リクエストの形式が正しくありません")
		return
	}
	valid := map[string]bool{"draft": true, "ready": true, "used": true}
	if !valid[req.Status] {
		writeError(w, http.StatusBadRequest, "status は draft / ready / used のいずれかです")
		return
	}
	if err := db.UpdateScenarioStatus(h.database, id, req.Status); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// send delivers a message to the clients watching sessionID. Every message
// carries "session_id". A client too slow to keep up loses messages; it
// reloads the session state when the turn ends.
func (h *Handler) send(sessionID int64, msg map[string]any) {
	msg["session_id"] = sessionID
	h.mu.RLock()
	defer h.mu.RUnlock()
	for client := range h.clients {
		if client.sessionID != 0 && client.sessionID != sessionID {
			continue
		}
		select {
		case client.sendC <- msg:
		default:
			log.Printf("[ws] dropped a %v message for a slow client", msg["type"])
		}
	}
}
