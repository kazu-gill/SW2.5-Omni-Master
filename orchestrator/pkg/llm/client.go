package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"strings"
	"time"
)

// Client communicates with one mlx-serve profile (OpenAI-compatible server).
// mlx-vlm rejects requests without "model" and reloads the resident model when
// a different name is sent, so every request carries the profile's model ID.
type Client struct {
	baseURL    string
	modelID    string
	httpClient *http.Client
	headers    map[string]string
}

func New(baseURL, modelID string, timeout time.Duration) *Client {
	return &Client{
		baseURL:    baseURL,
		modelID:    modelID,
		httpClient: &http.Client{Timeout: timeout},
	}
}

// WithHeader returns a copy of the client that adds header to every request,
// e.g. X-APC-Tenant to give this app its own prefix-cache entries on mlx-vlm.
func (c *Client) WithHeader(key, value string) *Client {
	cp := *c
	cp.headers = make(map[string]string, len(c.headers)+1)
	for k, v := range c.headers {
		cp.headers[k] = v
	}
	cp.headers[key] = value
	return &cp
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
	// OnDelta, when set, streams the reply: it is called with each piece of
	// answer text as it is generated (reasoning is not passed).
	OnDelta func(text string)
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
	Stream             bool               `json:"stream,omitempty"`
}

type streamChunk struct {
	Choices []struct {
		Delta struct {
			Content          string `json:"content"`
			ReasoningContent string `json:"reasoning_content"`
			Reasoning        string `json:"reasoning"`
		} `json:"delta"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
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
		Stream:             opts.OnDelta != nil,
	}
	if req.Stream {
		res, err := c.stream(ctx, req, opts.OnDelta)
		if err != nil {
			return Result{}, fmt.Errorf("llm.Complete: %w", err)
		}
		return res, nil
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

// stream reads an OpenAI server-sent-event stream ("data: {...}" lines
// ending with "data: [DONE]") and assembles the result.
func (c *Client) stream(ctx context.Context, req completionRequest, onDelta func(string)) (Result, error) {
	resp, err := c.post(ctx, "/v1/chat/completions", req)
	if err != nil {
		return Result{}, err
	}
	defer resp.Body.Close()

	var text, reasoning strings.Builder
	var res Result
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		data, ok := strings.CutPrefix(line, "data:")
		if !ok {
			continue
		}
		data = strings.TrimSpace(data)
		if data == "[DONE]" {
			break
		}
		var chunk streamChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			return Result{}, fmt.Errorf("decode stream: %w", err)
		}
		for _, ch := range chunk.Choices {
			if ch.Delta.Content != "" {
				text.WriteString(ch.Delta.Content)
				onDelta(ch.Delta.Content)
			}
			reasoning.WriteString(ch.Delta.ReasoningContent)
			reasoning.WriteString(ch.Delta.Reasoning)
			if ch.FinishReason != "" {
				res.FinishReason = ch.FinishReason
			}
		}
	}
	if err := sc.Err(); err != nil {
		return Result{}, fmt.Errorf("read stream: %w", err)
	}
	res.Text, res.Reasoning = text.String(), reasoning.String()
	return res, nil
}

// post sends a JSON request and returns the response when its status is 200.
func (c *Client) post(ctx context.Context, path string, reqBody any) (*http.Response, error) {
	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	for k, v := range c.headers {
		httpReq.Header.Set(k, v)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, b)
	}
	return resp, nil
}

func (c *Client) postJSON(ctx context.Context, path string, reqBody, respBody any) error {
	resp, err := c.post(ctx, path, reqBody)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(respBody); err != nil {
		return fmt.Errorf("decode: %w", err)
	}
	return nil
}
