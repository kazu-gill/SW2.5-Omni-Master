package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/kf/sw25-omni-master/pkg/db"
	"github.com/kf/sw25-omni-master/pkg/game"
)

// ── システムの状態 ───────────────────────────────────────────────────────────

func healthy(ctx context.Context, url string) bool {
	if url == "" {
		return false
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return false
	}
	resp, err := (&http.Client{Timeout: 2 * time.Second}).Do(req)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// handleSystem reports whether the mlx-serve profiles and the image studio
// answer, checked in parallel so the screen never waits on one slow probe.
func (h *Handler) handleSystem(w http.ResponseWriter, r *http.Request) {
	type ep struct {
		URL     string `json:"url"`
		ModelID string `json:"model_id,omitempty"`
		OK      bool   `json:"ok"`
	}
	var llmOK, embedOK, imgOK bool
	var wg sync.WaitGroup
	wg.Add(3)
	go func() { defer wg.Done(); llmOK = healthy(r.Context(), h.llmEP.URL+"/health") }()
	go func() { defer wg.Done(); embedOK = healthy(r.Context(), h.embedEP.URL+"/health") }()
	go func() { defer wg.Done(); imgOK = h.images != nil && h.images.Ping(r.Context()) }()
	wg.Wait()
	imagesURL := ""
	if h.images != nil {
		imagesURL = h.images.BaseURL()
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"llm":        ep{URL: h.llmEP.URL, ModelID: h.llmEP.ModelID, OK: llmOK},
		"embed":      ep{URL: h.embedEP.URL, ModelID: h.embedEP.ModelID, OK: embedOK},
		"images":     ep{URL: imagesURL, OK: imgOK},
		"apc_tenant": h.apcTenant,
	})
}

// ── ルール検索 ───────────────────────────────────────────────────────────────

// handleSearchRules runs the same search the GM's stage uses.
func (h *Handler) handleSearchRules(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		writeJSON(w, http.StatusOK, []game.RuleHit{})
		return
	}
	k := 5
	if n, err := strconv.Atoi(r.URL.Query().Get("k")); err == nil && n > 0 && n <= 20 {
		k = n
	}
	if h.rag == nil {
		writeError(w, http.StatusServiceUnavailable, "ルール検索が使えません")
		return
	}
	chunks, err := h.rag.Search(r.Context(), q, k)
	if err != nil {
		writeError(w, http.StatusBadGateway, "検索できませんでした。MLX ダッシュボードで埋め込みモデル (bge-m3-q8) が起動しているか確認してください: "+err.Error())
		return
	}
	hits := make([]game.RuleHit, 0, len(chunks))
	for _, c := range chunks {
		hits = append(hits, game.RuleHit{
			ID: c.ID, SourceType: c.SourceType, Section: c.Section, PageFrom: c.PageFrom, PageTo: c.PageTo,
			Text: c.Text, Score: c.Score, MatchedTerm: c.MatchedTerm,
		})
	}
	writeJSON(w, http.StatusOK, hits)
}

// ── シーン画像 ───────────────────────────────────────────────────────────────

var imageFileRe = regexp.MustCompile(`^\d+\.(png|jpg|jpeg|webp)$`)

func (h *Handler) sessionImageDir(sessionID int64) string {
	return filepath.Join(h.imagesDir, "sessions", strconv.FormatInt(sessionID, 10))
}

func (h *Handler) handleSessionImage(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	file := r.PathValue("file")
	if err != nil || !imageFileRe.MatchString(file) {
		writeError(w, http.StatusBadRequest, "画像の指定が正しくありません")
		return
	}
	http.ServeFile(w, r, filepath.Join(h.sessionImageDir(id), file))
}

// wantsImage decides when to illustrate: the first turn and every N-th turn
// after it. The rhythm is fixed in code rather than left to the model.
func (h *Handler) wantsImage(turn int) bool {
	return h.images != nil && h.imageEvery > 0 && (turn == 1 || turn%h.imageEvery == 0)
}

// makeSceneImage draws the turn's scene through the MLX image studio, saves
// it with the session and attaches it to the turn. Failures only log: the
// game goes on without a picture.
// sceneRequest is what the studio is asked to draw. One turn's narration
// often leaves the place unsaid (live run 2026-11-13: a fight in a mine shaft
// came out in a forest), so the story so far and the previous scene go along.
func (h *Handler) sceneRequest(sessionID int64, turn int, result *game.TurnResult) string {
	var b strings.Builder
	b.WriteString("ファンタジーTRPG（ソード・ワールド2.5）の一場面の挿絵を1枚。人物は後ろ姿か遠景にして、情景を中心に。場所と状況は下の流れに合わせる。\n")
	if summary, err := db.ListSummary(h.database, sessionID); err == nil && len(summary) > 0 {
		if len(summary) > 4 {
			summary = summary[len(summary)-4:]
		}
		b.WriteString("これまで: " + strings.Join(summary, " ") + "\n")
	}
	if prev, err := h.turnResult(sessionID, turn-1); err == nil && prev.GMNarration != "" {
		b.WriteString("直前の場面: " + clip(prev.GMNarration, 300) + "\n")
	}
	if result.Input != "" {
		b.WriteString("プレイヤーの行動: " + result.Input + "\n")
	}
	b.WriteString("この場面: " + result.GMNarration)
	return b.String()
}

func clip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

func (h *Handler) makeSceneImage(sessionID int64, turn int, result *game.TurnResult) {
	if !h.wantsImage(turn) {
		return
	}
	ev := wsEvents{h: h, sessionID: sessionID}
	ev.Stage(game.StageEvent{Turn: turn, Stage: "image", Label: "挿絵", Status: game.StageStart, Attempt: 1})
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()

	img, err := h.images.Scene(ctx, h.sceneRequest(sessionID, turn, result))
	if err != nil {
		log.Printf("[image] session %d turn %d: %v", sessionID, turn, err)
		ev.Stage(game.StageEvent{Turn: turn, Stage: "image", Label: "挿絵", Status: game.StageFailed, Attempt: 1, Message: err.Error()})
		return
	}
	dir := h.sessionImageDir(sessionID)
	name := fmt.Sprintf("%d%s", turn, img.Ext)
	if err := os.MkdirAll(dir, 0o755); err == nil {
		err = os.WriteFile(filepath.Join(dir, name), img.Data, 0o644)
	}
	url := fmt.Sprintf("/api/sessions/%d/images/%s", sessionID, name)

	// Attach to the stored turn, unless it was rewound meanwhile.
	if tr, err := h.turnResult(sessionID, turn); err == nil && tr.Input == result.Input {
		tr.ImageURL = url
		b, _ := json.Marshal(tr)
		_ = db.SaveTurnResult(h.database, sessionID, turn, string(b))
		ev.Stage(game.StageEvent{Turn: turn, Stage: "image", Label: "挿絵", Status: game.StageDone, Attempt: 1})
		h.send(sessionID, map[string]any{"type": "image", "turn": turn, "url": url})
	}
}
