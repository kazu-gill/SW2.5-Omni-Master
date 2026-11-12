// Package llmtest provides a fake mlx-serve (OpenAI-compatible) server for tests.
package llmtest

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"

	"github.com/kf/sw25-omni-master/pkg/llm"
)

// Request is what the fake server received for a chat completion.
type Request struct {
	Messages []llm.Message `json:"messages"`
	Stream   bool          `json:"stream"`
}

// Last is the content of the last message (the stage's instruction).
func (r Request) Last() string { return r.Messages[len(r.Messages)-1].Content }

// NewServer answers /v1/chat/completions with reply(req), streamed as
// server-sent events in two pieces when the request asks to stream, and
// /v1/embeddings with the vector [1,0,0].
func NewServer(reply func(Request) string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/embeddings":
			json.NewEncoder(w).Encode(map[string]any{
				"data": []map[string]any{{"embedding": []float32{1, 0, 0}}},
			})
			return
		case "/health":
			w.Write([]byte(`{"status":"ok"}`))
			return
		}
		var req Request
		json.NewDecoder(r.Body).Decode(&req)
		text := reply(req)
		if !req.Stream {
			json.NewEncoder(w).Encode(map[string]any{
				"choices": []map[string]any{
					{"message": map[string]string{"role": "assistant", "content": text}, "finish_reason": "stop"},
				},
			})
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		half := len([]rune(text)) / 2
		for _, part := range []string{string([]rune(text)[:half]), string([]rune(text)[half:])} {
			b, _ := json.Marshal(map[string]any{"choices": []map[string]any{{"delta": map[string]string{"content": part}}}})
			fmt.Fprintf(w, "data: %s\n\n", b)
		}
		fmt.Fprint(w, `data: {"choices":[{"delta":{},"finish_reason":"stop"}]}`+"\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
}

// ByStage answers each stage of a turn with a fixed reply, choosing by the
// stage's instruction: NPC, support (数値計算担当), summary (記録係) or GM.
func ByStage(gm, npc, support string) func(Request) string {
	return func(req Request) string {
		switch last := req.Last(); {
		case strings.Contains(last, "あなたはNPC"):
			return npc
		case strings.Contains(last, "数値計算担当"):
			return support
		case strings.Contains(last, "記録係"):
			return "- PCは坑道に入った。"
		default:
			return gm
		}
	}
}
