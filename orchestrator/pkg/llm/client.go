package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"time"
)

// Client communicates with one mlx-serve profile (OpenAI-compatible server).
// mlx-vlm rejects requests without "model" and reloads the resident model when
// a different name is sent, so every request carries the profile's model ID.
type Client struct {
	baseURL    string
	modelID    string
	httpClient *http.Client
}

func New(baseURL, modelID string, timeout time.Duration) *Client {
	return &Client{
		baseURL:    baseURL,
		modelID:    modelID,
		httpClient: &http.Client{Timeout: timeout},
	}
}

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type CompletionOptions struct {
	Temperature float64
	MaxTokens   int
	// Thinking lets the model reason before answering. The reasoning is
	// returned separately in Result.Reasoning and also consumes MaxTokens.
	Thinking bool
}

// Result is the assistant's reply to a chat-completion request.
type Result struct {
	Text         string
	Reasoning    string
	FinishReason string // "stop", or "length" when cut off by MaxTokens
}

type chatTemplateKwargs struct {
	EnableThinking bool `json:"enable_thinking"`
}

type completionRequest struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	Temperature float64   `json:"temperature,omitempty"`
	MaxTokens   int       `json:"max_tokens,omitempty"`
	// mlx-vlm returns identical text for identical input unless a seed is sent.
	Seed int64 `json:"seed"`
	// mlx-vlm reads the top-level flag; mlx-lm and llama read chat_template_kwargs.
	EnableThinking     bool               `json:"enable_thinking"`
	ChatTemplateKwargs chatTemplateKwargs `json:"chat_template_kwargs"`
}

type completionResponse struct {
	Choices []struct {
		Message struct {
			Content          string `json:"content"`
			ReasoningContent string `json:"reasoning_content"`
			Reasoning        string `json:"reasoning"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
}

// Complete sends a chat-completion request and returns the assistant's reply.
func (c *Client) Complete(ctx context.Context, messages []Message, opts CompletionOptions) (Result, error) {
	req := completionRequest{
		Model:              c.modelID,
		Messages:           messages,
		Temperature:        opts.Temperature,
		MaxTokens:          opts.MaxTokens,
		Seed:               rand.Int64N(1 << 31),
		EnableThinking:     opts.Thinking,
		ChatTemplateKwargs: chatTemplateKwargs{EnableThinking: opts.Thinking},
	}
	var cr completionResponse
	if err := c.postJSON(ctx, "/v1/chat/completions", req, &cr); err != nil {
		return Result{}, fmt.Errorf("llm.Complete: %w", err)
	}
	if len(cr.Choices) == 0 {
		return Result{}, fmt.Errorf("llm.Complete: empty choices")
	}
	ch := cr.Choices[0]
	reasoning := ch.Message.ReasoningContent
	if reasoning == "" {
		reasoning = ch.Message.Reasoning
	}
	return Result{Text: ch.Message.Content, Reasoning: reasoning, FinishReason: ch.FinishReason}, nil
}

type embeddingRequest struct {
	Model string `json:"model"`
	Input string `json:"input"`
}

type embeddingResponse struct {
	Data []struct {
		Embedding []float32 `json:"embedding"`
	} `json:"data"`
}

// Embed returns the embedding vector for the given text.
func (c *Client) Embed(ctx context.Context, text string) ([]float32, error) {
	var er embeddingResponse
	if err := c.postJSON(ctx, "/v1/embeddings", embeddingRequest{Model: c.modelID, Input: text}, &er); err != nil {
		return nil, fmt.Errorf("llm.Embed: %w", err)
	}
	if len(er.Data) == 0 {
		return nil, fmt.Errorf("llm.Embed: empty data")
	}
	return er.Data[0].Embedding, nil
}

func (c *Client) postJSON(ctx context.Context, path string, reqBody, respBody any) error {
	body, err := json.Marshal(reqBody)
	if err != nil {
		return err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("status %d: %s", resp.StatusCode, b)
	}
	if err := json.NewDecoder(resp.Body).Decode(respBody); err != nil {
		return fmt.Errorf("decode: %w", err)
	}
	return nil
}
