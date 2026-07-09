// Package ai talks to any OpenAI-compatible chat-completions endpoint
// (OpenAI, Gemini's /v1beta/openai surface, Azure, Ollama, …) — the only
// provider assumptions are the /chat/completions path and its SSE format.
package ai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"specquill/server/internal/config"
)

type Message struct {
	Role    string `json:"role"` // system | user | assistant
	Content string `json:"content"`
}

type Client struct {
	baseURL string
	model   string // main (thinking-class): chat, draft edits
	quick   string // fast one-shot tier: commit messages, titles
	key     string
	budget  int // grounding system-prompt cap in bytes (0 = package default)
	http    *http.Client
}

func New(cfg config.AIConfig) *Client {
	key := ""
	if cfg.APIKeyEnv != "" {
		key = os.Getenv(cfg.APIKeyEnv)
	}
	quick := cfg.QuickModel
	if quick == "" {
		quick = cfg.Model
	}
	return &Client{
		baseURL: strings.TrimRight(cfg.BaseURL, "/"),
		model:   cfg.Model,
		quick:   quick,
		key:     key,
		budget:  cfg.GroundingBudget,
		http:    &http.Client{Timeout: 5 * time.Minute},
	}
}

func (c *Client) Model() string      { return c.model }
func (c *Client) QuickModel() string { return c.quick }

// GroundingBudget is the configured system-prompt cap in bytes (0 = default).
func (c *Client) GroundingBudget() int { return c.budget }

func (c *Client) request(ctx context.Context, body any) (*http.Response, error) {
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.key != "" {
		req.Header.Set("Authorization", "Bearer "+c.key)
	}
	res, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	if res.StatusCode != http.StatusOK {
		defer res.Body.Close()
		raw, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
		return nil, fmt.Errorf("ai provider %d: %s", res.StatusCode, strings.TrimSpace(string(raw)))
	}
	return res, nil
}

// Stream sends the conversation and invokes onDelta for each content chunk.
func (c *Client) Stream(ctx context.Context, msgs []Message, onDelta func(delta string) error) error {
	res, err := c.request(ctx, map[string]any{
		"model": c.model, "messages": msgs, "stream": true,
	})
	if err != nil {
		return err
	}
	defer res.Body.Close()

	scanner := bufio.NewScanner(res.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "[DONE]" {
			return nil
		}
		var chunk struct {
			Choices []struct {
				Delta struct {
					Content string `json:"content"`
				} `json:"delta"`
			} `json:"choices"`
		}
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			continue // ignore keep-alives / unknown events
		}
		if len(chunk.Choices) > 0 && chunk.Choices[0].Delta.Content != "" {
			if err := onDelta(chunk.Choices[0].Delta.Content); err != nil {
				return err
			}
		}
	}
	return scanner.Err()
}

// Complete sends the conversation to the main model and returns the content.
func (c *Client) Complete(ctx context.Context, msgs []Message) (string, error) {
	return c.complete(ctx, c.model, msgs)
}

// QuickComplete runs a one-shot task on the fast tier (quick_model).
func (c *Client) QuickComplete(ctx context.Context, msgs []Message) (string, error) {
	return c.complete(ctx, c.quick, msgs)
}

func (c *Client) complete(ctx context.Context, model string, msgs []Message) (string, error) {
	res, err := c.request(ctx, map[string]any{
		"model": model, "messages": msgs, "stream": false,
	})
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		return "", err
	}
	if len(out.Choices) == 0 {
		return "", fmt.Errorf("ai provider returned no choices")
	}
	return out.Choices[0].Message.Content, nil
}

// ExtractJSON tolerantly pulls a JSON object out of a model reply that may be
// wrapped in code fences or prose.
func ExtractJSON(reply string, v any) error {
	s := strings.TrimSpace(reply)
	if i := strings.Index(s, "```"); i >= 0 {
		s = s[i+3:]
		s = strings.TrimPrefix(s, "json")
		if j := strings.Index(s, "```"); j >= 0 {
			s = s[:j]
		}
	}
	start := strings.IndexByte(s, '{')
	end := strings.LastIndexByte(s, '}')
	if start < 0 || end <= start {
		return fmt.Errorf("no JSON object in model reply")
	}
	return json.Unmarshal([]byte(s[start:end+1]), v)
}
