package ai

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"specquill/server/internal/config"
)

func TestExtractJSONTakesTheFirstCompleteObject(t *testing.T) {
	type out struct {
		Findings []string `json:"findings"`
	}
	cases := map[string]string{
		"plain":                 `{"findings":["a"]}`,
		"fenced":                "```json\n{\"findings\":[\"a\"]}\n```",
		"prose around":          `Here you go: {"findings":["a"]} — hope that helps.`,
		"a second object after": "{\"findings\":[\"a\"]}\n{\"findings\":[\"ignored\"]}",
		"brace inside a string": `{"findings":["a{b"]}`,
	}
	for name, reply := range cases {
		var got out
		if err := ExtractJSON(reply, &got); err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if len(got.Findings) != 1 || got.Findings[0][:1] != "a" {
			t.Errorf("%s: got %v", name, got.Findings)
		}
	}
	// a preamble object that does not fit the shape must not hide the answer
	var got struct {
		Answer int `json:"answer"`
	}
	if err := ExtractJSON(`{"thinking":"…"} {"answer":42}`, &got); err != nil || got.Answer != 42 {
		t.Errorf("preamble object: got %v (%v)", got.Answer, err)
	}
	if err := ExtractJSON("no json here", &got); err == nil {
		t.Error("a reply without JSON must error")
	}
}

// testClient points a real Client at a stub provider, with the retry pause
// collapsed so the test does not sleep.
func testClient(url string) *Client {
	c := New(config.AIConfig{BaseURL: url, Model: "m"})
	c.retryBase = time.Millisecond
	return c
}

const okBody = `{"choices":[{"message":{"content":"hi"}}]}`

func TestCompleteRetriesTransientProviderFailures(t *testing.T) {
	for _, tc := range []struct {
		name  string
		code  int
		tries int // requests the provider should see
		ok    bool
	}{
		{"502 recovers", http.StatusBadGateway, 2, true},
		{"429 recovers", http.StatusTooManyRequests, 2, true},
		{"503 recovers", http.StatusServiceUnavailable, 2, true},
		{"400 is not retried", http.StatusBadRequest, 1, false},
		{"401 is not retried", http.StatusUnauthorized, 1, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var got int
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				got++
				if got < tc.tries {
					http.Error(w, `{"error":{"code":"server_error"}}`, tc.code)
					return
				}
				if !tc.ok {
					http.Error(w, `{"error":{"code":"bad"}}`, tc.code)
					return
				}
				fmt.Fprint(w, okBody)
			}))
			defer srv.Close()

			reply, err := testClient(srv.URL).Complete(t.Context(), []Message{{Role: "user", Content: "x"}})
			if tc.ok && (err != nil || reply != "hi") {
				t.Fatalf("want a recovered answer, got %q (%v)", reply, err)
			}
			if !tc.ok && err == nil {
				t.Fatal("want an error")
			}
			if got != tc.tries {
				t.Errorf("provider saw %d request(s), want %d", got, tc.tries)
			}
		})
	}
}

func TestCompleteGivesUpAfterTheAttemptBudget(t *testing.T) {
	var got int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got++
		http.Error(w, "boom", http.StatusBadGateway)
	}))
	defer srv.Close()

	c := testClient(srv.URL)
	if _, err := c.Complete(t.Context(), []Message{{Role: "user"}}); err == nil {
		t.Fatal("a provider that never recovers must surface the error")
	} else if !strings.Contains(err.Error(), "502") {
		t.Errorf("the error must name the provider status, got %v", err)
	}
	if got != c.attempts {
		t.Errorf("provider saw %d request(s), want the %d-attempt budget", got, c.attempts)
	}
}

func TestRetryStopsWhenTheRunIsCancelled(t *testing.T) {
	var got int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got++
		http.Error(w, "boom", http.StatusBadGateway)
	}))
	defer srv.Close()

	c := testClient(srv.URL)
	c.retryBase = 30 * time.Second // a retry that actually waits
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { _, err := c.Complete(ctx, []Message{{Role: "user"}}); done <- err }()
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("cancelling the run must not wait out the backoff")
	}
	if got > 1 {
		t.Errorf("a cancelled run kept retrying (%d requests)", got)
	}
}

func TestRetryAfterHeaderIsHonoured(t *testing.T) {
	for _, tc := range []struct {
		header string
		want   time.Duration
	}{
		{"2", 2 * time.Second},
		{"", 0},
		{"Wed, 21 Oct 2026 07:28:00 GMT", 0}, // HTTP-date form: ignored, not misparsed
		{"99999", 0},                         // absurd: fall back to our own backoff
	} {
		err := &statusErr{code: 429, retryAfter: tc.header}
		if got := retryAfter(err); got != tc.want {
			t.Errorf("Retry-After %q → %v, want %v", tc.header, got, tc.want)
		}
	}
}
