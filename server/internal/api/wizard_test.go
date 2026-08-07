package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"specquill/server/internal/ai"
	"specquill/server/internal/auth"
	"specquill/server/internal/config"
	"specquill/server/internal/gitx"
	"specquill/server/internal/store"
)

// fakeProvider is an OpenAI-compatible endpoint that replays canned turns.
// Each entry is either plain content or a tool call followed by more turns —
// enough to exercise the round loop and the JSON buffer reset.
type fakeTurn struct {
	content string
	tool    string // tool name; empty = terminal content turn
	args    string
}

type fakeProvider struct {
	mu    sync.Mutex
	turns []fakeTurn
	seen  int
	// systems records the system prompt of every request, so tests can assert
	// what actually reached the model
	systems []string
}

func (f *fakeProvider) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
		Tools []any `json:"tools"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)

	f.mu.Lock()
	if len(body.Messages) > 0 && body.Messages[0].Role == "system" {
		f.systems = append(f.systems, body.Messages[0].Content)
	}
	turn := fakeTurn{content: "{}"}
	if f.seen < len(f.turns) {
		turn = f.turns[f.seen]
	}
	f.seen++
	f.mu.Unlock()

	w.Header().Set("Content-Type", "text/event-stream")
	flusher := w.(http.Flusher)
	frame := func(v any) {
		raw, _ := json.Marshal(v)
		fmt.Fprintf(w, "data: %s\n\n", raw)
		flusher.Flush()
	}
	if turn.content != "" {
		frame(map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"content": turn.content}}}})
	}
	if turn.tool != "" {
		frame(map[string]any{"choices": []any{map[string]any{"delta": map[string]any{
			"tool_calls": []any{map[string]any{
				"index": 0, "id": "call_1", "type": "function",
				"function": map[string]any{"name": turn.tool, "arguments": turn.args},
			}},
		}}}})
	}
	fmt.Fprint(w, "data: [DONE]\n\n")
	flusher.Flush()
}

// testWizardServer builds a server with one writable project "w" holding a
// couple of documents, backed by the given canned provider turns.
func testWizardServer(t *testing.T, turns ...fakeTurn) (http.Handler, *fakeProvider) {
	t.Helper()
	tmp := t.TempDir()
	src := filepath.Join(tmp, "src")
	gitRun(t, "init", "-b", "main", src)
	for rel, content := range map[string]string{
		"specs/retention.md":      "---\ntype: Specification\ntitle: Retention rules\n---\n\n# Retention rules\n\nRecords are retained for 5 years.\n",
		"requirements/REQ-001.md": "---\ntype: Requirement\nid: REQ-001\ntitle: Keep records\n---\n\n# Keep records\n",
	} {
		abs := filepath.Join(src, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	gitRun(t, "-C", src, "-c", "user.name=t", "-c", "user.email=t@t", "add", "-A")
	gitRun(t, "-C", src, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-m", "init")

	fake := &fakeProvider{turns: turns}
	provider := httptest.NewServer(fake)
	t.Cleanup(provider.Close)

	cfg := &config.Config{
		DataDir: filepath.Join(tmp, "data"),
		Git:     config.GitConfig{CommitterName: "svc", CommitterEmail: "svc@t"},
		Session: config.SessionConfig{TTL: time.Hour, CookieSecure: false},
		Auth:    config.AuthConfig{Local: config.LocalAuthConfig{Enabled: true}},
		Repos:   []config.RepoConfig{{ID: "w", Mode: config.Writable, Remote: src, DefaultBranch: "main"}},
	}
	st := store.OpenTest(t)
	if err := st.SyncProjects([]store.Project{{ProjectID: "w", RepoID: "w"}}); err != nil {
		t.Fatal(err)
	}
	hash, _ := auth.HashPassword("hunter2secret")
	if err := st.AddLocalUser("flo", "Flo Test", "flo@test.local", hash); err != nil {
		t.Fatal(err)
	}
	git, err := gitx.NewManager(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := git.Init(); err != nil {
		t.Fatal(err)
	}
	h := New(cfg, git, Options{
		Store:    st,
		Sessions: auth.NewSessions(st, cfg),
		AI:       ai.New(config.AIConfig{Enabled: true, BaseURL: provider.URL, Model: "mock-1"}),
		Dist:     fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("<html></html>")}},
	})
	return h, fake
}

// wizardPost drives one wizard endpoint and returns the parsed SSE frames:
// the terminal result object, the tool events, and any error frame.
func wizardPost(t *testing.T, h http.Handler, cookie *http.Cookie, url string, body any) (result map[string]any, notes []string, errMsg string) {
	t.Helper()
	var buf bytes.Buffer
	_ = json.NewEncoder(&buf).Encode(body)
	req := httptest.NewRequest("POST", url, &buf)
	req.Header.Set("X-SpecQuill", "1")
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("%s: HTTP %d: %s", url, rec.Code, rec.Body.String())
	}
	for _, frame := range strings.Split(rec.Body.String(), "\n\n") {
		line := strings.TrimSpace(frame)
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		var payload map[string]any
		if json.Unmarshal([]byte(strings.TrimPrefix(line, "data:")), &payload) != nil {
			continue
		}
		if r, ok := payload["result"].(map[string]any); ok {
			result = r
		}
		if n, ok := payload["note"].(string); ok {
			notes = append(notes, n)
		}
		if e, ok := payload["error"].(string); ok {
			errMsg = e
		}
	}
	return result, notes, errMsg
}

func TestWizardInterviewStructuresTheTurn(t *testing.T) {
	reply := `{"reply":"Retention is already specified at 5 years in specs/retention.md.",
		"questions":["Does the new rule replace the 5-year window?"],
		"rubric":[{"criterion":"Retention period stated","met":false},{"criterion":"Scope named","met":true}],
		"readyToDraft":true}`
	h, fake := testWizardServer(t, fakeTurn{tool: "search", args: `{"query":"retention"}`}, fakeTurn{content: reply})
	cookie := login(t, h)

	result, notes, errMsg := wizardPost(t, h, cookie, "/api/repos/w/speccy/interview", map[string]any{
		"intent": "extend the retention window", "family": "spec", "folder": "specs/",
	})
	if errMsg != "" {
		t.Fatalf("interview errored: %s", errMsg)
	}
	// askJSON narrates each tool call; the SPA renders the strings as chips
	if len(notes) != 1 || !strings.Contains(notes[0], "search") {
		t.Fatalf("tool narration not streamed: %+v", notes)
	}
	rubric, _ := result["rubric"].([]any)
	if len(rubric) != 2 {
		t.Fatalf("rubric = %+v", result["rubric"])
	}
	// the first turn only has the author's rough idea — a model claiming
	// readiness there has skipped the interview, so the server overrides it
	if result["readyToDraft"] != false {
		t.Fatalf("readyToDraft not forced false on the first turn: %+v", result)
	}
	// the stage prompt reached the model, and no write/ask tooling did
	sys := fake.systems[0]
	if !strings.Contains(sys, "readyToDraft") || !strings.Contains(sys, "# Workspace files") {
		t.Fatal("interview prompt missing its contract or the grounding")
	}
	if strings.Contains(sys, "# Editing rules") {
		t.Fatal("read-only stage was handed the editing rules")
	}
}

func TestWizardInterviewKeepsReadinessOnLaterTurns(t *testing.T) {
	reply := `{"reply":"Got it.","questions":[],"rubric":[{"criterion":"Scope named","met":true}],"readyToDraft":true}`
	h, _ := testWizardServer(t, fakeTurn{content: reply})
	cookie := login(t, h)

	result, _, errMsg := wizardPost(t, h, cookie, "/api/repos/w/speccy/interview", map[string]any{
		"intent": "extend the retention window", "family": "spec",
		"messages": []map[string]string{
			{"role": "assistant", "content": "how long?"},
			{"role": "user", "content": "seven years"},
		},
	})
	if errMsg != "" {
		t.Fatalf("interview errored: %s", errMsg)
	}
	if result["readyToDraft"] != true {
		t.Fatalf("readiness dropped once the transcript had answers: %+v", result)
	}
}

func TestWizardComposeNormalizesAgainstTheOutline(t *testing.T) {
	// out of order, re-cased, one requested block missing, one extra
	reply := `{"title":"Seven-year retention","sections":[
		{"name":"edge cases","content":"Legal hold."},
		{"name":"Overview","content":"Records live seven years."},
		{"name":"Assumptions","content":"Existing records are re-stamped."}]}`
	h, _ := testWizardServer(t, fakeTurn{content: reply})
	cookie := login(t, h)

	result, _, errMsg := wizardPost(t, h, cookie, "/api/repos/w/speccy/compose", map[string]any{
		"intent": "extend the retention window", "family": "spec",
		"sections": []string{"Overview", "Behaviour", "Edge cases"},
	})
	if errMsg != "" {
		t.Fatalf("compose errored: %s", errMsg)
	}
	if result["title"] != "Seven-year retention" {
		t.Fatalf("title = %v", result["title"])
	}
	sections, _ := result["sections"].([]any)
	if len(sections) != 4 {
		t.Fatalf("sections = %+v", sections)
	}
	names := make([]string, 0, len(sections))
	for _, s := range sections {
		names = append(names, s.(map[string]any)["name"].(string))
	}
	want := []string{"Overview", "Behaviour", "Edge cases", "Assumptions"}
	for i, n := range want {
		if names[i] != n {
			t.Fatalf("section order = %v, want %v", names, want)
		}
	}
	// the block the model skipped is present and empty — the UI shows the gap
	if sections[1].(map[string]any)["content"] != "" {
		t.Fatalf("missing block was invented: %+v", sections[1])
	}
}

func TestWizardRelatedDropsPathsThatDoNotExist(t *testing.T) {
	reply := `{"matches":[
		{"path":"specs/retention.md","title":"Retention rules","relation":"overlaps","reason":"states the 5-year window"},
		{"path":"specs/imaginary.md","title":"Ghost","relation":"covers","reason":"hallucinated"}],
		"recommendation":"specs/imaginary.md"}`
	h, _ := testWizardServer(t, fakeTurn{content: reply})
	cookie := login(t, h)

	result, _, errMsg := wizardPost(t, h, cookie, "/api/repos/w/speccy/related", map[string]any{
		"intent": "extend the retention window", "family": "spec",
	})
	if errMsg != "" {
		t.Fatalf("related errored: %s", errMsg)
	}
	matches, _ := result["matches"].([]any)
	if len(matches) != 1 || matches[0].(map[string]any)["path"] != "specs/retention.md" {
		t.Fatalf("hallucinated path survived: %+v", matches)
	}
	// the recommendation pointed at the dropped match — it must not dangle
	if result["recommendation"] != "new" {
		t.Fatalf("recommendation = %v, want new", result["recommendation"])
	}
}

func TestWizardRetriesWhenTheModelWrapsTheJSONInProse(t *testing.T) {
	h, fake := testWizardServer(t,
		fakeTurn{content: "Sure! Here is my analysis, which is definitely not JSON."},
		fakeTurn{content: `{"content":"Tightened.","note":"cut two sentences"}`},
	)
	cookie := login(t, h)

	result, _, errMsg := wizardPost(t, h, cookie, "/api/repos/w/speccy/section", map[string]any{
		"intent": "extend the retention window", "family": "spec",
		"title": "Seven-year retention", "section": "Overview",
		"sectionContent": "Long text.", "instruction": "tighten",
	})
	if errMsg != "" {
		t.Fatalf("section errored: %s", errMsg)
	}
	if result["content"] != "Tightened." {
		t.Fatalf("retry result = %+v", result)
	}
	if fake.seen != 2 {
		t.Fatalf("expected exactly one corrective retry, provider saw %d calls", fake.seen)
	}
}

func TestWizardSurfacesUnparseableRepliesAsAnErrorFrame(t *testing.T) {
	h, _ := testWizardServer(t,
		fakeTurn{content: "not json"},
		fakeTurn{content: "still not json"},
	)
	cookie := login(t, h)

	result, _, errMsg := wizardPost(t, h, cookie, "/api/repos/w/speccy/related", map[string]any{
		"intent": "something", "family": "spec",
	})
	if errMsg == "" {
		t.Fatalf("a model that never emits JSON must fail loudly, got result %+v", result)
	}
	if !strings.Contains(errMsg, "JSON") {
		t.Fatalf("error frame = %q", errMsg)
	}
}

func TestWizardRequiresAnIntent(t *testing.T) {
	h, _ := testWizardServer(t)
	cookie := login(t, h)
	code, out := doJSON(t, h, cookie, "POST", "/api/repos/w/speccy/interview", map[string]any{"family": "spec"})
	if code != http.StatusBadRequest {
		t.Fatalf("empty intent accepted: %d %v", code, out)
	}
}

// The guided stages must never be handed write or ask tooling: they are
// read-only by construction and collect questions structurally.
func TestWizardToolsAreReadOnly(t *testing.T) {
	tb, _ := toolboxFixture(t, false) // writable: true — must not matter here
	names := map[string]bool{}
	for _, s := range tb.readSpecs() {
		names[s.Name] = true
	}
	for _, banned := range []string{"edit_file", "create_file", "ask_user"} {
		if names[banned] {
			t.Fatalf("read-only tool set exposes %s", banned)
		}
	}
	for _, want := range []string{"read_file", "list_files", "search"} {
		if !names[want] {
			t.Fatalf("read-only tool set is missing %s", want)
		}
	}
}
