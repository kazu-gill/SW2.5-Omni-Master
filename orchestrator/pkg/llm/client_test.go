package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func mockServer(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return New(srv.URL, "test-model", 5*time.Second)
}

func chatReply(w http.ResponseWriter, content, reasoning, finish string) {
	msg := map[string]any{"role": "assistant", "content": content}
	if reasoning != "" {
		msg["reasoning_content"] = reasoning
	}
	json.NewEncoder(w).Encode(map[string]any{
		"choices": []map[string]any{{"message": msg, "finish_reason": finish}},
	})
}

// captureRequest decodes the request body into a generic map for inspection.
func captureRequest(t *testing.T, r *http.Request) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		t.Fatalf("decode request: %v", err)
	}
	return body
}

func TestComplete(t *testing.T) {
	c := mockServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		chatReply(w, "テスト応答", "", "stop")
	})

	got, err := c.Complete(context.Background(), []Message{{Role: "user", Content: "hello"}}, CompletionOptions{})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if got.Text != "テスト応答" {
		t.Errorf("Text = %q, want テスト応答", got.Text)
	}
	if got.FinishReason != "stop" {
		t.Errorf("FinishReason = %q, want stop", got.FinishReason)
	}
}

func TestCompleteSendsModelSeedAndThinkingOff(t *testing.T) {
	var body map[string]any
	c := mockServer(t, func(w http.ResponseWriter, r *http.Request) {
		body = captureRequest(t, r)
		chatReply(w, "ok", "", "stop")
	})

	if _, err := c.Complete(context.Background(), []Message{{Role: "user", Content: "x"}}, CompletionOptions{Temperature: 0.7, MaxTokens: 100}); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if body["model"] != "test-model" {
		t.Errorf("model = %v, want test-model", body["model"])
	}
	if _, ok := body["seed"].(float64); !ok {
		t.Errorf("seed missing or not a number: %v", body["seed"])
	}
	if body["enable_thinking"] != false {
		t.Errorf("enable_thinking = %v, want false", body["enable_thinking"])
	}
	kw, _ := body["chat_template_kwargs"].(map[string]any)
	if kw["enable_thinking"] != false {
		t.Errorf("chat_template_kwargs.enable_thinking = %v, want false", kw["enable_thinking"])
	}
	if body["temperature"] != 0.7 || body["max_tokens"] != float64(100) {
		t.Errorf("temperature/max_tokens = %v/%v", body["temperature"], body["max_tokens"])
	}
}

func TestCompleteSeedVariesPerRequest(t *testing.T) {
	seeds := map[float64]bool{}
	c := mockServer(t, func(w http.ResponseWriter, r *http.Request) {
		seeds[captureRequest(t, r)["seed"].(float64)] = true
		chatReply(w, "ok", "", "stop")
	})
	for range 5 {
		if _, err := c.Complete(context.Background(), []Message{{Role: "user", Content: "x"}}, CompletionOptions{}); err != nil {
			t.Fatalf("Complete: %v", err)
		}
	}
	if len(seeds) < 2 {
		t.Errorf("seed did not vary across 5 requests: %v", seeds)
	}
}

func TestCompleteThinkingOn(t *testing.T) {
	var body map[string]any
	c := mockServer(t, func(w http.ResponseWriter, r *http.Request) {
		body = captureRequest(t, r)
		chatReply(w, "391", "17*23 を計算する", "stop")
	})

	got, err := c.Complete(context.Background(), []Message{{Role: "user", Content: "x"}}, CompletionOptions{Thinking: true})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if body["enable_thinking"] != true {
		t.Errorf("enable_thinking = %v, want true", body["enable_thinking"])
	}
	kw, _ := body["chat_template_kwargs"].(map[string]any)
	if kw["enable_thinking"] != true {
		t.Errorf("chat_template_kwargs.enable_thinking = %v, want true", kw["enable_thinking"])
	}
	if got.Text != "391" || got.Reasoning != "17*23 を計算する" {
		t.Errorf("got %+v", got)
	}
}

func TestCompleteHTTPError(t *testing.T) {
	c := mockServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal error", http.StatusInternalServerError)
	})
	_, err := c.Complete(context.Background(), []Message{{Role: "user", Content: "x"}}, CompletionOptions{})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestEmbed(t *testing.T) {
	var body map[string]any
	c := mockServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/embeddings" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		body = captureRequest(t, r)
		json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{{"index": 0, "embedding": []float32{0.1, 0.2, 0.3}}},
		})
	})

	vec, err := c.Embed(context.Background(), "test")
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(vec) != 3 {
		t.Errorf("embedding length = %d, want 3", len(vec))
	}
	if body["model"] != "test-model" || body["input"] != "test" {
		t.Errorf("request = %v", body)
	}
}

func TestEmbedEmptyData(t *testing.T) {
	c := mockServer(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"data": []any{}})
	})
	if _, err := c.Embed(context.Background(), "test"); err == nil {
		t.Fatal("expected error for empty data, got nil")
	}
}
