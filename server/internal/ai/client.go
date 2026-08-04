// Package ai talks to any OpenAI-compatible chat-completions endpoint
// (OpenAI, Gemini's /v1beta/openai surface, Azure, Ollama, …) — the only
// provider assumptions are the /chat/completions path and its SSE format.
package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"reflect"
	"strconv"
	"strings"
	"time"

	"specquill/server/internal/config"
)

type Message struct {
	Role    string `json:"role"` // system | user | assistant | tool
	Content string `json:"content"`
	// tool-calling round-trip (omitted for plain text messages): an assistant
	// message carries the calls it requested, a tool message answers one call
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

// defaultMaxCallsPerRun bounds one alignment run's model calls when the
// deployment sets no ceiling of its own.
const defaultMaxCallsPerRun = 500

type Client struct {
	baseURL string
	model   string // main (thinking-class): chat, draft edits
	quick   string // fast one-shot tier: commit messages, titles
	// models a recipe may additionally name per stage (ai.models). NOT a
	// fallback list — an id outside it fails recipe validation.
	models   []string
	maxCalls int // ceiling on model calls per alignment run (0 = package default)
	key      string
	budget   int    // grounding system-prompt cap in bytes (0 = package default)
	effort   string // reasoning_effort passthrough ("" = omit from requests)
	http     *http.Client

	// transient-failure retry: attempts total, with an exponentially growing
	// pause from retryBase (fields, so tests don't sleep)
	attempts  int
	retryBase time.Duration
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
		baseURL:   strings.TrimRight(cfg.BaseURL, "/"),
		model:     cfg.Model,
		quick:     quick,
		models:    cfg.Models,
		maxCalls:  cfg.MaxCallsPerRun,
		key:       key,
		budget:    cfg.GroundingBudget,
		effort:    cfg.ReasoningEffort,
		http:      &http.Client{Timeout: 5 * time.Minute},
		attempts:  3,
		retryBase: time.Second,
	}
}

// chatBody assembles a chat-completions request. The configured
// reasoning_effort rides along when set — OpenAI reasoning models default it
// server-side and then refuse function tools unless it is explicitly "none".
func (c *Client) chatBody(model string, msgs []Message, stream bool) map[string]any {
	body := map[string]any{"model": model, "messages": msgs, "stream": stream}
	if c.effort != "" {
		body["reasoning_effort"] = c.effort
	}
	return body
}

func (c *Client) Model() string      { return c.model }
func (c *Client) QuickModel() string { return c.quick }

// WithModel returns a client that talks to a DIFFERENT model, sharing this
// one's transport, key and retry policy. An alignment recipe may name a model
// per stage — a cheap one to survey, a thinking-class one to judge — and this
// is the whole seam that makes it possible: every existing call site keeps
// using the configured tier untouched.
//
// The tier names are accepted as aliases so a recipe never has to hardcode a
// deployment's model ids. An unknown id is NOT resolved here — the recipe
// validator checks it against ai.models before a run starts, so reaching this
// with an arbitrary string is a programming error, not user input.
func (c *Client) WithModel(id string) *Client {
	switch id {
	case "", "default", c.model:
		return c
	case "quick":
		id = c.quick
	}
	if id == c.model {
		return c
	}
	out := *c
	out.model = id
	return &out
}

// Models is the set of model ids a recipe may name: both configured tiers plus
// the explicit ai.models allowlist. Recipes are user content committed to a
// repository, so what they can point the server at is deployment policy.
func (c *Client) Models() []string {
	seen := map[string]bool{}
	var out []string
	for _, m := range append([]string{c.model, c.quick}, c.models...) {
		if m != "" && !seen[m] {
			seen[m] = true
			out = append(out, m)
		}
	}
	return out
}

// AllowedModels is Models as a set, for recipe validation.
func (c *Client) AllowedModels() map[string]bool {
	out := map[string]bool{}
	for _, m := range c.Models() {
		out[m] = true
	}
	return out
}

// MaxCallsPerRun is the hard ceiling on model calls one alignment run may
// make (0 = the package default). A recipe multiplies stages by items by
// units, so an author's typo is otherwise measured in hours and money.
func (c *Client) MaxCallsPerRun() int {
	if c.maxCalls > 0 {
		return c.maxCalls
	}
	return defaultMaxCallsPerRun
}

// GroundingBudget is the configured system-prompt cap in bytes (0 = default).
func (c *Client) GroundingBudget() int { return c.budget }

// request posts a chat-completions body, working around one provider quirk:
// OpenAI reasoning models (gpt-5.x) refuse function tools on /chat/completions
// unless reasoning_effort is explicitly "none" — they DEFAULT the field
// server-side, so omitting it does not help. On that specific 400 the request
// is retried once with reasoning_effort forced to "none" (an explicit
// ai.reasoning_effort config skips the wasted round trip).
func (c *Client) request(ctx context.Context, body map[string]any) (*http.Response, error) {
	res, err := c.attempt(ctx, body)
	if err == nil || body["reasoning_effort"] == "none" || !strings.Contains(err.Error(), "reasoning_effort") {
		return res, err
	}
	retry := make(map[string]any, len(body)+1)
	for k, v := range body {
		retry[k] = v
	}
	retry["reasoning_effort"] = "none"
	return c.attempt(ctx, retry)
}

// attempt posts the body, retrying TRANSIENT failures with a growing pause:
// providers 502/503/429 under load and a dropped connection is not an answer.
// A long drift run makes hundreds of calls, and one blip used to sink a whole
// unit. Only retryable failures are repeated — a 400 is deterministic, and a
// cancelled context (the user stopped the run) must not sleep.
func (c *Client) attempt(ctx context.Context, body map[string]any) (*http.Response, error) {
	attempts := max(c.attempts, 1)
	var err error
	for i := 0; ; i++ {
		var res *http.Response
		res, err = c.post(ctx, body)
		if err == nil || i == attempts-1 || !retryable(err) || ctx.Err() != nil {
			return res, err
		}
		wait := c.retryBase << i
		if hint := retryAfter(err); hint > 0 {
			wait = hint
		}
		log.Printf("ai: %s%s failed (%v) — retry %d/%d in %s", body["model"], labelOf(ctx), err, i+1, attempts-1, wait.Round(time.Millisecond))
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(wait):
		}
	}
}

// statusErr is a non-200 provider answer; the status decides retryability.
type statusErr struct {
	code       int
	body       string
	retryAfter string
}

func (e *statusErr) Error() string { return fmt.Sprintf("ai provider %d: %s", e.code, e.body) }

// retryable: provider-side or transport failures that a second try can fix —
// 429 (rate limit), 408, any 5xx, and network errors (reset/EOF/timeout).
// Everything else (400 bad request, 401, 404 …) fails the same way twice.
func retryable(err error) bool {
	var se *statusErr
	if errors.As(err, &se) {
		return se.code == http.StatusTooManyRequests || se.code == http.StatusRequestTimeout || se.code >= 500
	}
	return !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded)
}

// retryAfter honours the provider's own pacing hint (delta-seconds only; the
// HTTP-date form is rare here and a wrong parse would sleep for hours).
func retryAfter(err error) time.Duration {
	var se *statusErr
	if !errors.As(err, &se) || se.retryAfter == "" {
		return 0
	}
	secs, convErr := strconv.Atoi(strings.TrimSpace(se.retryAfter))
	if convErr != nil || secs <= 0 || secs > 120 {
		return 0
	}
	return time.Duration(secs) * time.Second
}

func (c *Client) post(ctx context.Context, body map[string]any) (*http.Response, error) {
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
		return nil, &statusErr{code: res.StatusCode, body: strings.TrimSpace(string(raw)), retryAfter: res.Header.Get("Retry-After")}
	}
	return res, nil
}

// Stream sends the conversation and invokes onDelta for each content chunk.
func (c *Client) Stream(ctx context.Context, msgs []Message, onDelta func(delta string) error) error {
	res, err := c.request(ctx, c.chatBody(c.model, msgs, true))
	if err != nil {
		return err
	}
	defer res.Body.Close()
	return scanSSE(res, func(payload string) error {
		if perr := providerErr(payload); perr != nil {
			return perr
		}
		var chunk struct {
			Choices []struct {
				Delta struct {
					Content string `json:"content"`
				} `json:"delta"`
			} `json:"choices"`
		}
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			return nil // ignore keep-alives / unknown events
		}
		if len(chunk.Choices) > 0 && chunk.Choices[0].Delta.Content != "" {
			return onDelta(chunk.Choices[0].Delta.Content)
		}
		return nil
	})
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
	started := time.Now()
	res, err := c.request(ctx, c.chatBody(model, msgs, false))
	if err != nil {
		log.Printf("ai: %s%s complete failed after %s: %v", model, labelOf(ctx), since(started), err)
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
		log.Printf("ai: %s%s complete returned no choices after %s", model, labelOf(ctx), since(started))
		return "", fmt.Errorf("ai provider returned no choices")
	}
	reply := out.Choices[0].Message.Content
	// sizes, never content: prompts carry workspace material
	log.Printf("ai: %s%s complete in %s (prompt %s → reply %s)",
		model, labelOf(ctx), since(started), size(msgLen(msgs)), size(len(reply)))
	return reply, nil
}

// msgLen is the prompt size a call carries, for the log.
func msgLen(msgs []Message) int {
	n := 0
	for _, m := range msgs {
		n += len(m.Content)
	}
	return n
}

func size(n int) string {
	if n < 1024 {
		return fmt.Sprintf("%dB", n)
	}
	return fmt.Sprintf("%.1fKB", float64(n)/1024)
}

func since(t time.Time) time.Duration { return time.Since(t).Round(time.Millisecond) }

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
	// Decode the first object that FITS: models sometimes emit a second object
	// (a repeat, or a commentary blob) after the answer — spanning first-`{` to
	// last-`}` is then invalid JSON ("invalid character '{' after top-level
	// value") — and sometimes a preamble object before it, which decodes
	// "successfully" into an empty struct and silently turns a failed call into
	// an empty result. So: prefer the first object sharing a field with the
	// target, and fall back to the first that decodes at all.
	want := jsonFields(v)
	var firstErr error
	fallback := -1
	for off := 0; ; {
		i := strings.IndexByte(s[off:], '{')
		if i < 0 {
			break
		}
		start := off + i
		var probe map[string]json.RawMessage
		if err := json.NewDecoder(strings.NewReader(s[start:])).Decode(&probe); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			off = start + 1
			continue
		}
		if fallback < 0 {
			fallback = start
		}
		if len(want) == 0 || fits(probe, want) {
			return json.NewDecoder(strings.NewReader(s[start:])).Decode(v)
		}
		off = start + 1
	}
	if fallback >= 0 {
		return json.NewDecoder(strings.NewReader(s[fallback:])).Decode(v)
	}
	if firstErr != nil {
		return firstErr
	}
	return fmt.Errorf("no JSON object in model reply")
}

// jsonFields lists the json field names of a struct (pointer) target, so
// ExtractJSON can tell the answer from a preamble object. Empty for map or
// slice targets — anything decodes into those, and any object will do.
func jsonFields(v any) map[string]bool {
	t := reflect.TypeOf(v)
	for t != nil && t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t == nil || t.Kind() != reflect.Struct {
		return nil
	}
	out := map[string]bool{}
	for i := 0; i < t.NumField(); i++ {
		name, _, _ := strings.Cut(t.Field(i).Tag.Get("json"), ",")
		if name != "" && name != "-" {
			out[name] = true
		}
	}
	return out
}

func fits(probe map[string]json.RawMessage, want map[string]bool) bool {
	for k := range probe {
		if want[k] {
			return true
		}
	}
	return false
}
