package game

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// ComfyUIClient sends image generation requests to the ComfyUI server.
type ComfyUIClient struct {
	baseURL    string
	httpClient *http.Client
}

func NewComfyUIClient(baseURL string) *ComfyUIClient {
	if baseURL == "" {
		return nil
	}
	return &ComfyUIClient{
		baseURL:    baseURL,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

type comfyPromptRequest struct {
	Prompt map[string]any `json:"prompt"`
}

type promptResp struct {
	PromptID string `json:"prompt_id"`
}

type historyResp map[string]historyEntry

type historyEntry struct {
	Outputs map[string]struct {
		Images []struct {
			Filename string `json:"filename"`
			Type     string `json:"type"`
		} `json:"images"`
	} `json:"outputs"`
	Status struct {
		Completed bool `json:"completed"`
	} `json:"status"`
}

// RequestImage queues an image generation job and returns the prompt_id.
func (c *ComfyUIClient) RequestImage(ctx context.Context, prompt string) (string, error) {
	payload := comfyPromptRequest{
		Prompt: map[string]any{
			"3": map[string]any{
				"class_type": "KSampler",
				"inputs":     map[string]any{"positive": prompt},
			},
		},
	}
	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/prompt", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("comfyui.RequestImage: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("comfyui.RequestImage: status %d", resp.StatusCode)
	}

	var pr promptResp
	if err := json.NewDecoder(resp.Body).Decode(&pr); err != nil {
		return "", fmt.Errorf("comfyui.RequestImage: decode prompt_id: %w", err)
	}
	if pr.PromptID == "" {
		return "", fmt.Errorf("comfyui.RequestImage: empty prompt_id")
	}
	return pr.PromptID, nil
}

// PollImage polls /history/{promptID} every 2s until the image is ready (max 120s).
// Returns the ComfyUI view URL for the generated image.
func (c *ComfyUIClient) PollImage(ctx context.Context, promptID string) (string, error) {
	deadline := time.Now().Add(120 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(2 * time.Second):
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/history/"+promptID, nil)
		if err != nil {
			return "", err
		}
		resp, err := c.httpClient.Do(req)
		if err != nil {
			continue // transient error, keep polling
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			continue
		}

		var hr historyResp
		if err := json.Unmarshal(body, &hr); err != nil {
			continue
		}
		entry, ok := hr[promptID]
		if !ok || !entry.Status.Completed {
			continue
		}
		for _, node := range entry.Outputs {
			for _, img := range node.Images {
				if img.Filename != "" && img.Type == "output" {
					return fmt.Sprintf("%s/view?filename=%s&type=output", c.baseURL, img.Filename), nil
				}
			}
		}
	}
	return "", fmt.Errorf("comfyui.PollImage: timeout after 120s (prompt_id=%s)", promptID)
}

// BaseURL returns the configured ComfyUI server URL.
func (c *ComfyUIClient) BaseURL() string { return c.baseURL }

// Ping checks whether the ComfyUI server is reachable.
func (c *ComfyUIClient) Ping(ctx context.Context) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/system_stats", nil)
	if err != nil {
		return false
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}
