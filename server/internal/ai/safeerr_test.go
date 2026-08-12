package ai

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

// A provider's 400 body routinely echoes the offending request; for the speccy
// that request carries the grounding prompt, i.e. workspace document content.
// It must never reach a log line.
func TestSafeErrWithholdsTheProviderBody(t *testing.T) {
	secret := "retention policy: clients are notified at ..."
	err := fmt.Errorf("wrapped: %w", &statusErr{code: 400, body: `{"error":"bad input near '` + secret + `'"}`})
	got := SafeErr(err)
	if strings.Contains(got, secret) {
		t.Fatalf("provider body leaked into the log line: %q", got)
	}
	if !strings.Contains(got, "400") {
		t.Fatalf("status code lost, log is useless: %q", got)
	}
	// our own errors keep their detail — that is the diagnostic value
	if SafeErr(errors.New("model reply was not JSON: invalid character 'B'")) !=
		"model reply was not JSON: invalid character 'B'" {
		t.Fatal("non-provider error was degraded")
	}
	if SafeErr(nil) != "" {
		t.Fatal("nil should render empty")
	}
}
