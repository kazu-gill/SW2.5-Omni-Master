package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func mockServer(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return New(srv.URL)
}

func TestComplete(t *testing.T) {
	c := mockServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]string{"role": "assistant", "content": "テスト応答"}},
			},
		})
	})

	got, err := c.Complete(context.Background(), []Message{{Role: "user", Content: "hello"}}, CompletionOptions{})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if got != "テスト応答" {
		t.Errorf("got %q, want テスト応答", got)
	}
}

func TestCompleteParallel(t *testing.T) {
	callCount := 0
	c := mockServer(t, func(w http.ResponseWriter, r *http.Request) {
		callCount++
		json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]string{"role": "assistant", "content": "ok"}},
			},
		})
	})

	reqs := [][]Message{
		{{Role: "user", Content: "A"}},
		{{Role: "user", Content: "B"}},
		{{Role: "user", Content: "C"}},
	}
	results := c.CompleteParallel(context.Background(), reqs, CompletionOptions{})
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}
	for _, r := range results {
		if r.Err != nil {
			t.Errorf("result[%d] error: %v", r.Index, r.Err)
		}
	}
}

func TestEmbed(t *testing.T) {
	c := mockServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/embedding" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		json.NewEncoder(w).Encode(map[string]any{
			"embedding": []float32{0.1, 0.2, 0.3},
		})
	})

	vec, err := c.Embed(context.Background(), "test")
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(vec) != 3 {
		t.Errorf("embedding length = %d, want 3", len(vec))
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
