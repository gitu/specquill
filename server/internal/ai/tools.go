// Tool-calling loop over the OpenAI-compatible streaming API. The client
// stays transport-only: WHAT the tools do lives with the caller (api package),
// this file handles the wire format, fragment accumulation and the round loop.
package ai

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// ToolFunc is the function half of a tool call.
type ToolFunc struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"` // JSON-encoded by the model
}

// ToolCall is one function invocation the model requested.
type ToolCall struct {
	ID       string   `json:"id"`
	Type     string   `json:"type"` // always "function"
	Function ToolFunc `json:"function"`
}

// ToolSpec declares one callable function (OpenAI `tools` entry).
type ToolSpec struct {
	Name        string
	Description string
	Parameters  map[string]any // JSON schema for the arguments object
}

// ToolExec runs one tool call and returns its result text. halt=true stops
// the loop with this call left unanswered — the caller surfaces it to the
// user (ask-a-question) and resumes the conversation in a later request.
type ToolExec func(name, args string) (result string, halt bool, err error)

const (
	maxToolRounds     = 8         // model turns that may request tools
	maxToolBytes      = 64 * 1024 // total tool-result budget per conversation
	maxToolResultSize = 24 * 1024 // single result cap (read_file of a big doc)
)

// StreamTools streams a conversation that may call tools. Content deltas go
// to onDelta; each requested call is announced via onCall (display), executed
// through exec, and its result appended as a tool message before the next
// round. When exec halts (pending user question), the loop stops and returns
// (resume, pending): resume is every message appended beyond msgs — the
// caller replays them plus the user's answer to continue statelessly.
func (c *Client) StreamTools(ctx context.Context, msgs []Message, tools []ToolSpec, exec ToolExec, onDelta func(string) error, onCall func(ToolCall, string, error) error) (resume []Message, pending *ToolCall, err error) {
	if len(tools) == 0 {
		return nil, nil, c.Stream(ctx, msgs, onDelta)
	}
	specs := make([]map[string]any, 0, len(tools))
	for _, t := range tools {
		specs = append(specs, map[string]any{
			"type":     "function",
			"function": map[string]any{"name": t.Name, "description": t.Description, "parameters": t.Parameters},
		})
	}
	conv := append([]Message{}, msgs...)
	used := 0
	for round := 0; ; round++ {
		body := c.chatBody(c.model, conv, true)
		// budget exhausted → last request goes out tool-less so the model
		// must answer with what it has instead of looping forever
		if round < maxToolRounds && used < maxToolBytes {
			body["tools"] = specs
		}
		content, calls, err := c.streamOnce(ctx, body, onDelta)
		if err != nil {
			return nil, nil, err
		}
		if len(calls) == 0 {
			return conv[len(msgs):], nil, nil
		}
		conv = append(conv, Message{Role: "assistant", Content: content, ToolCalls: calls})
		for _, tc := range calls {
			if pending != nil {
				// a question to the user interrupted this batch — later
				// siblings are answered without executing so the transcript
				// stays well-formed when the conversation resumes
				conv = append(conv, Message{Role: "tool", ToolCallID: tc.ID,
					Content: "(not executed: waiting for the user's answer to your question)"})
				continue
			}
			result, halt, execErr := exec(tc.Function.Name, tc.Function.Arguments)
			if onCall != nil {
				if err := onCall(tc, result, execErr); err != nil {
					return nil, nil, err
				}
			}
			if halt {
				p := tc
				pending = &p
				continue // its tool message comes from the user
			}
			if execErr != nil {
				result = "ERROR: " + execErr.Error()
			}
			if len(result) > maxToolResultSize {
				result = result[:maxToolResultSize] + "\n… (truncated)"
			}
			used += len(result)
			conv = append(conv, Message{Role: "tool", ToolCallID: tc.ID, Content: result})
		}
		if pending != nil {
			return conv[len(msgs):], pending, nil
		}
	}
}

// streamOnce performs a single streaming request, forwarding content deltas
// and accumulating tool-call fragments (OpenAI streams id/name/arguments in
// pieces keyed by index).
func (c *Client) streamOnce(ctx context.Context, body map[string]any, onDelta func(string) error) (content string, calls []ToolCall, err error) {
	res, err := c.request(ctx, body)
	if err != nil {
		return "", nil, err
	}
	defer res.Body.Close()

	byIndex := map[int]*ToolCall{}
	order := []int{}
	if err := scanSSE(res, func(payload string) error {
		var chunk struct {
			Choices []struct {
				Delta struct {
					Content   string `json:"content"`
					ToolCalls []struct {
						Index    int    `json:"index"`
						ID       string `json:"id"`
						Type     string `json:"type"`
						Function struct {
							Name      string `json:"name"`
							Arguments string `json:"arguments"`
						} `json:"function"`
					} `json:"tool_calls"`
				} `json:"delta"`
			} `json:"choices"`
		}
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil || len(chunk.Choices) == 0 {
			return nil // keep-alives / unknown events
		}
		d := chunk.Choices[0].Delta
		if d.Content != "" {
			content += d.Content
			if err := onDelta(d.Content); err != nil {
				return err
			}
		}
		for _, f := range d.ToolCalls {
			tc, ok := byIndex[f.Index]
			if !ok {
				tc = &ToolCall{Type: "function"}
				byIndex[f.Index] = tc
				order = append(order, f.Index)
			}
			if f.ID != "" {
				tc.ID = f.ID
			}
			if f.Function.Name != "" {
				tc.Function.Name += f.Function.Name
			}
			tc.Function.Arguments += f.Function.Arguments
		}
		return nil
	}); err != nil {
		return "", nil, err
	}
	for i, idx := range order {
		tc := *byIndex[idx]
		if tc.ID == "" { // providers that omit ids (some local runtimes)
			tc.ID = fmt.Sprintf("call_%d", i)
		}
		calls = append(calls, tc)
	}
	return content, calls, nil
}

// scanSSE walks an SSE body, invoking fn per data payload until [DONE].
func scanSSE(res *http.Response, fn func(payload string) error) error {
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
		if err := fn(payload); err != nil {
			return err
		}
	}
	return scanner.Err()
}
