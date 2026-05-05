package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

// Client communicates with a single llama-server instance.
type Client struct {
	baseURL    string
	httpClient *http.Client
}

func New(baseURL string) *Client {
	return &Client{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 120 * time.Second,
		},
	}
}

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type CompletionOptions struct {
	Temperature float64 `json:"temperature,omitempty"`
	MaxTokens   int     `json:"max_tokens,omitempty"`
}

type completionRequest struct {
	Messages    []Message `json:"messages"`
	Temperature float64   `json:"temperature,omitempty"`
	MaxTokens   int       `json:"max_tokens,omitempty"`
}

type completionResponse struct {
	Choices []struct {
		Message Message `json:"message"`
	} `json:"choices"`
}

// Complete sends a chat-completion request and returns the assistant's reply.
func (c *Client) Complete(ctx context.Context, messages []Message, opts CompletionOptions) (string, error) {
	req := completionRequest{
		Messages:    messages,
		Temperature: opts.Temperature,
		MaxTokens:   opts.MaxTokens,
	}
	body, _ := json.Marshal(req)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("llm.Complete: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("llm.Complete: status %d: %s", resp.StatusCode, b)
	}

	var cr completionResponse
	if err := json.NewDecoder(resp.Body).Decode(&cr); err != nil {
		return "", fmt.Errorf("llm.Complete decode: %w", err)
	}
	if len(cr.Choices) == 0 {
		return "", fmt.Errorf("llm.Complete: empty choices")
	}
	return cr.Choices[0].Message.Content, nil
}

// ParallelResult holds the result for one parallel request.
type ParallelResult struct {
	Index  int
	Text   string
	Err    error
}

// CompleteParallel sends multiple chat-completion requests concurrently.
// Results are returned in request order.
func (c *Client) CompleteParallel(ctx context.Context, requests [][]Message, opts CompletionOptions) []ParallelResult {
	results := make([]ParallelResult, len(requests))
	var wg sync.WaitGroup
	for i, msgs := range requests {
		wg.Add(1)
		go func(idx int, m []Message) {
			defer wg.Done()
			text, err := c.Complete(ctx, m, opts)
			results[idx] = ParallelResult{Index: idx, Text: text, Err: err}
		}(i, msgs)
	}
	wg.Wait()
	return results
}

type embeddingRequest struct {
	Content string `json:"content"`
}

type embeddingResponse struct {
	Embedding []float32 `json:"embedding"`
}

// Embed returns the embedding vector for the given text.
func (c *Client) Embed(ctx context.Context, text string) ([]float32, error) {
	body, _ := json.Marshal(embeddingRequest{Content: text})
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/embedding", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("llm.Embed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("llm.Embed: status %d: %s", resp.StatusCode, b)
	}

	var er embeddingResponse
	if err := json.NewDecoder(resp.Body).Decode(&er); err != nil {
		return nil, fmt.Errorf("llm.Embed decode: %w", err)
	}
	return er.Embedding, nil
}
