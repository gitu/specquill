package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"specquill/server/internal/config"
)

// sseChunk renders one streaming chunk the way OpenAI-compatible providers do.
func sseChunk(t *testing.T, delta map[string]any) string {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": delta}}})
	if err != nil {
		t.Fatal(err)
	}
	return "data: " + string(raw) + "\n\n"
}

func toolFragment(index int, id, name, args string) map[string]any {
	f := map[string]any{}
	if name != "" {
		f["name"] = name
	}
	if args != "" {
		f["arguments"] = args
	}
	tc := map[string]any{"index": index, "function": f}
	if id != "" {
		tc["id"] = id
	}
	return map[string]any{"tool_calls": []any{tc}}
}

// fakeProvider scripts responses per request; it records the request bodies.
func fakeProvider(t *testing.T, responses []string) (*Client, *[]map[string]any) {
	t.Helper()
	var seen []map[string]any
	i := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		seen = append(seen, body)
		w.Header().Set("Content-Type", "text/event-stream")
		if i >= len(responses) {
			t.Errorf("unexpected request #%d", i+1)
			return
		}
		fmt.Fprint(w, responses[i])
		i++
	}))
	t.Cleanup(srv.Close)
	return New(config.AIConfig{BaseURL: srv.URL, Model: "test-1"}), &seen
}

func TestStreamToolsRunsFragmentedCallsThenAnswers(t *testing.T) {
	round1 := sseChunk(t, toolFragment(0, "call_1", "edit_file", `{"path":`)) +
		sseChunk(t, toolFragment(0, "", "", `"specs/a.md"}`)) +
		"data: [DONE]\n\n"
	round2 := sseChunk(t, map[string]any{"content": "Edited "}) +
		sseChunk(t, map[string]any{"content": "the spec."}) +
		"data: [DONE]\n\n"
	c, seen := fakeProvider(t, []string{round1, round2})

	var execName, execArgs, streamed string
	resume, pending, err := c.StreamTools(context.Background(),
		[]Message{{Role: "user", Content: "edit it"}},
		[]ToolSpec{{Name: "edit_file", Parameters: map[string]any{"type": "object"}}},
		func(name, args string) (string, bool, error) {
			execName, execArgs = name, args
			return "ok: saved", false, nil
		},
		func(d string) error { streamed += d; return nil },
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if pending != nil {
		t.Fatalf("unexpected pending call %+v", pending)
	}
	if execName != "edit_file" || execArgs != `{"path":"specs/a.md"}` {
		t.Fatalf("fragmented call not reassembled: %s %s", execName, execArgs)
	}
	if streamed != "Edited the spec." {
		t.Fatalf("streamed %q", streamed)
	}
	// second request must replay the assistant tool_calls message + result
	msgs := (*seen)[1]["messages"].([]any)
	last := msgs[len(msgs)-1].(map[string]any)
	if last["role"] != "tool" || last["tool_call_id"] != "call_1" || last["content"] != "ok: saved" {
		t.Fatalf("tool result not replayed: %+v", last)
	}
	if resume == nil {
		t.Fatal("resume messages missing")
	}
}

func TestStreamToolsHaltReturnsPendingAndAnswersSiblings(t *testing.T) {
	round1 := sseChunk(t, toolFragment(0, "call_a", "ask_user", `{"question":"Which?"}`)) +
		sseChunk(t, toolFragment(1, "call_b", "edit_file", `{"path":"x"}`)) +
		"data: [DONE]\n\n"
	c, _ := fakeProvider(t, []string{round1})

	execd := []string{}
	resume, pending, err := c.StreamTools(context.Background(),
		[]Message{{Role: "user", Content: "go"}},
		[]ToolSpec{{Name: "ask_user"}, {Name: "edit_file"}},
		func(name, args string) (string, bool, error) {
			execd = append(execd, name)
			if name == "ask_user" {
				return "", true, nil
			}
			return "done", false, nil
		},
		func(string) error { return nil }, nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if pending == nil || pending.Function.Name != "ask_user" || pending.ID != "call_a" {
		t.Fatalf("pending = %+v", pending)
	}
	if len(execd) != 1 { // the sibling after the halt must NOT execute
		t.Fatalf("executed %v", execd)
	}
	// resume = assistant msg + deferred sibling answer; the ask itself stays open
	if len(resume) != 2 || resume[0].Role != "assistant" || resume[1].ToolCallID != "call_b" {
		t.Fatalf("resume = %+v", resume)
	}
	if !strings.Contains(resume[1].Content, "not executed") {
		t.Fatalf("sibling answer = %q", resume[1].Content)
	}
}

func TestReasoningEffortPassthrough(t *testing.T) {
	answer := sseChunk(t, map[string]any{"content": "hi"}) + "data: [DONE]\n\n"
	srvBodies := func(effort string) []map[string]any {
		var seen []map[string]any
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			seen = append(seen, body)
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, answer)
		}))
		defer srv.Close()
		c := New(config.AIConfig{BaseURL: srv.URL, Model: "m", ReasoningEffort: effort})
		_, _, err := c.StreamTools(context.Background(), []Message{{Role: "user", Content: "x"}},
			[]ToolSpec{{Name: "read_file"}},
			func(string, string) (string, bool, error) { return "", false, nil },
			func(string) error { return nil }, nil)
		if err != nil {
			t.Fatal(err)
		}
		return seen
	}
	// configured → sent (gpt-5.x refuses tools without an explicit "none")
	if got := srvBodies("none")[0]["reasoning_effort"]; got != "none" {
		t.Fatalf("reasoning_effort not sent: %v", got)
	}
	// unconfigured → omitted (local providers reject unknown params)
	if _, ok := srvBodies("")[0]["reasoning_effort"]; ok {
		t.Fatal("reasoning_effort sent despite empty config")
	}
}

// Mid-stream provider errors arrive as {"error": {...}} data payloads — the
// delta parser used to skip them as unknown events, silently ending the
// stream with no text and no trace.
func TestStreamToolsSurfacesMidStreamProviderError(t *testing.T) {
	round := "data: " + `{"error":{"message":"The server had an error processing your request."}}` + "\n\n" +
		"data: [DONE]\n\n"
	c, _ := fakeProvider(t, []string{round})
	_, _, err := c.StreamTools(context.Background(), []Message{{Role: "user", Content: "x"}},
		[]ToolSpec{{Name: "read_file"}},
		func(string, string) (string, bool, error) { return "", false, nil },
		func(string) error { return nil }, nil)
	if err == nil || !strings.Contains(err.Error(), "server had an error") {
		t.Fatalf("mid-stream error swallowed: %v", err)
	}
}

// A terminal round with no content and no tool calls is a provider failure
// (token cap, filtered output) — it must error, not end silently.
func TestStreamToolsEmptyReplyErrors(t *testing.T) {
	round := sseChunk(t, map[string]any{}) + "data: [DONE]\n\n"
	c, _ := fakeProvider(t, []string{round})
	_, _, err := c.StreamTools(context.Background(), []Message{{Role: "user", Content: "x"}},
		[]ToolSpec{{Name: "read_file"}},
		func(string, string) (string, bool, error) { return "", false, nil },
		func(string) error { return nil }, nil)
	if err == nil || !strings.Contains(err.Error(), "empty reply") {
		t.Fatalf("empty reply not surfaced: %v", err)
	}
}

// Some OpenAI-compatible runtimes resend the function name with every
// arguments fragment — set-once accumulation must not concatenate it.
func TestStreamToolsRepeatedNameFragments(t *testing.T) {
	round1 := sseChunk(t, toolFragment(0, "c1", "edit_file", `{"pa`)) +
		sseChunk(t, toolFragment(0, "", "edit_file", `th":"x"}`)) +
		"data: [DONE]\n\n"
	round2 := sseChunk(t, map[string]any{"content": "done"}) + "data: [DONE]\n\n"
	c, _ := fakeProvider(t, []string{round1, round2})
	var name string
	_, _, err := c.StreamTools(context.Background(), []Message{{Role: "user", Content: "x"}},
		[]ToolSpec{{Name: "edit_file"}},
		func(n, _ string) (string, bool, error) { name = n; return "ok", false, nil },
		func(string) error { return nil }, nil)
	if err != nil || name != "edit_file" {
		t.Fatalf("name accumulated wrong: %q (%v)", name, err)
	}
}

// gpt-5.x refuses function tools on /chat/completions with its server-side
// default reasoning_effort — the client must retry once with an explicit
// "none" instead of surfacing the 400.
func TestReasoningToolsConflictRetriesWithNone(t *testing.T) {
	refusal := `{"error":{"message":"Function tools with reasoning_effort are not supported for gpt-5.6-terra in /v1/chat/completions. To use function tools, use /v1/responses or set reasoning_effort to 'none'.","type":"invalid_request_error","param":"reasoning_effort"}}`
	var seen []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		seen = append(seen, body)
		if body["reasoning_effort"] != "none" {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprint(w, refusal)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, sseChunk(t, map[string]any{"content": "worked"})+"data: [DONE]\n\n")
	}))
	defer srv.Close()

	c := New(config.AIConfig{BaseURL: srv.URL, Model: "gpt-5.6-terra"})
	var streamed string
	_, _, err := c.StreamTools(context.Background(), []Message{{Role: "user", Content: "x"}},
		[]ToolSpec{{Name: "read_file"}},
		func(string, string) (string, bool, error) { return "", false, nil },
		func(d string) error { streamed += d; return nil }, nil)
	if err != nil {
		t.Fatalf("conflict not retried: %v", err)
	}
	if streamed != "worked" {
		t.Fatalf("streamed %q", streamed)
	}
	if len(seen) != 2 || seen[1]["reasoning_effort"] != "none" {
		t.Fatalf("expected refused attempt + none retry, got %d requests", len(seen))
	}
}

func TestStreamToolsIterationCapForcesAnswer(t *testing.T) {
	loop := sseChunk(t, toolFragment(0, "c", "read_file", `{"path":"a"}`)) + "data: [DONE]\n\n"
	responses := make([]string, 0, maxToolRounds+1)
	for i := 0; i < maxToolRounds; i++ {
		responses = append(responses, loop)
	}
	responses = append(responses, sseChunk(t, map[string]any{"content": "final"})+"data: [DONE]\n\n")
	c, seen := fakeProvider(t, responses)

	_, pending, err := c.StreamTools(context.Background(),
		[]Message{{Role: "user", Content: "go"}},
		[]ToolSpec{{Name: "read_file"}},
		func(string, string) (string, bool, error) { return "data", false, nil },
		func(string) error { return nil }, nil,
	)
	if err != nil || pending != nil {
		t.Fatal(err, pending)
	}
	final := (*seen)[len(*seen)-1]
	if _, hasTools := final["tools"]; hasTools {
		t.Fatal("capped round must go out tool-less")
	}
}
