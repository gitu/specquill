package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// sseStream is a server-sent-events response: JSON frames plus a heartbeat.
//
// The heartbeat is not optional decoration. Thinking-class models stay silent
// for minutes; without bytes on the wire, reverse proxies close the connection
// at their idle timeout and the feature "just stops" with nothing in any log.
// SSE comments (": ping") are invisible to the client parser.
type sseStream struct {
	mu      sync.Mutex
	w       http.ResponseWriter
	flusher http.Flusher
	done    chan struct{}
}

const sseHeartbeat = 15 * time.Second

// startSSE writes the event-stream headers and starts the heartbeat. The
// caller must defer Close. Returns false (after writing an error) when the
// ResponseWriter cannot stream.
func startSSE(w http.ResponseWriter) (*sseStream, bool) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		jsonError(w, http.StatusInternalServerError, "streaming unsupported")
		return nil, false
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	s := &sseStream{w: w, flusher: flusher, done: make(chan struct{})}
	go func() {
		t := time.NewTicker(sseHeartbeat)
		defer t.Stop()
		for {
			select {
			case <-s.done:
				return
			case <-t.C:
				s.mu.Lock()
				fmt.Fprint(s.w, ": ping\n\n")
				s.flusher.Flush()
				s.mu.Unlock()
			}
		}
	}()
	return s, true
}

// Send writes one JSON data frame.
func (s *sseStream) Send(v any) {
	raw, err := json.Marshal(v)
	if err != nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	fmt.Fprintf(s.w, "data: %s\n\n", raw)
	s.flusher.Flush()
}

// Close stops the heartbeat.
func (s *sseStream) Close() { close(s.done) }
