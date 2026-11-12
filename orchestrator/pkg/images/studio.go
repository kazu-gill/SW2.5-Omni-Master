// Package images draws scene illustrations through the MLX platform's image
// studio (mlx-studio-web). The studio's Gemma turns a request into tags,
// style and view (/api/studio/infer) and ComfyUI draws them
// (/api/studio/render), so Omni-Master needs no ComfyUI workflow of its own.
package images

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path"
	"strings"
	"time"
)

// Client talks to mlx-studio-web.
type Client struct {
	baseURL string
	http    *http.Client
	poll    time.Duration
}

// New returns a client, or nil when baseURL is empty (images off).
func New(baseURL string) *Client {
	if baseURL == "" {
		return nil
	}
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		http:    &http.Client{Timeout: 60 * time.Second},
		poll:    2 * time.Second,
	}
}

// BaseURL is the studio's address.
func (c *Client) BaseURL() string { return c.baseURL }

// Ping reports whether the studio answers its health check.
func (c *Client) Ping(ctx context.Context) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/health", nil)
	if err != nil {
		return false
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// Image is a drawn picture.
type Image struct {
	Data []byte
	Ext  string // ".png" など
}

// Scene asks the studio to plan one picture for request and draws it.
func (c *Client) Scene(ctx context.Context, request string) (*Image, error) {
	var plan struct {
		Images []struct {
			Tags  string `json:"tags"`
			Style string `json:"style"`
			View  string `json:"view"`
			Look  string `json:"look"`
		} `json:"images"`
	}
	if err := c.job(ctx, "/api/studio/infer", map[string]any{
		"request": request, "character": "", "history": []any{},
	}, &plan); err != nil {
		return nil, fmt.Errorf("構図を決められませんでした: %w", err)
	}
	if len(plan.Images) == 0 || plan.Images[0].Tags == "" {
		return nil, fmt.Errorf("構図が空でした")
	}
	p := plan.Images[0]
	var drawn struct {
		URL string `json:"url"`
	}
	if err := c.job(ctx, "/api/studio/render", map[string]any{
		"tags": p.Tags, "style": p.Style, "view": p.View, "look": p.Look, "character": "",
	}, &drawn); err != nil {
		return nil, fmt.Errorf("描けませんでした: %w", err)
	}
	if drawn.URL == "" {
		return nil, fmt.Errorf("画像の URL が返りませんでした")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+drawn.URL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("画像の取得: status %d", resp.StatusCode)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	ext := path.Ext(drawn.URL)
	if ext == "" {
		ext = ".png"
	}
	return &Image{Data: data, Ext: ext}, nil
}

// job POSTs with an X-Job-Id. The studio answers within ~20 s, or 202 with
// the job id; then GET /api/job/{id} is asked until the result is ready. The
// POST is never sent twice (the platform's job convention).
func (c *Client) job(ctx context.Context, route string, body any, out any) error {
	id := newJobID()
	b, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+route, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Job-Id", id)
	for {
		resp, err := c.http.Do(req)
		if err != nil {
			return err
		}
		raw, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		switch resp.StatusCode {
		case http.StatusOK:
			return json.Unmarshal(raw, out)
		case http.StatusAccepted:
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(c.poll):
			}
			req, err = http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/job/"+id, nil)
			if err != nil {
				return err
			}
		default:
			var e struct {
				Error string `json:"error"`
			}
			if json.Unmarshal(raw, &e) == nil && e.Error != "" {
				return fmt.Errorf("%s", e.Error)
			}
			return fmt.Errorf("status %d", resp.StatusCode)
		}
	}
}

func newJobID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return "sw25-" + hex.EncodeToString(b)
}
