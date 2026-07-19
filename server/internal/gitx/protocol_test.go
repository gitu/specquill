package gitx

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A remote value carries its transport in the string itself, so `git clone`'s
// "--" separator can't neuter the ext:: helper — only the GIT_ALLOW_PROTOCOL
// allowlist set in runFull does. This proves the helper never executes.
func TestCloneRefusesExtTransport(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "pwned")
	// ext::<cmd> would run <cmd> as the transport; touch the marker if it does
	remote := "ext::sh -c \"touch " + marker + "\""

	_, _, err := runFull("", nil, nil, "clone", "--bare", "--", remote, filepath.Join(dir, "clone.git"))
	if err == nil {
		t.Fatal("clone of an ext:: remote succeeded; transport allowlist not enforced")
	}
	if !strings.Contains(err.Error(), "protocol") && !strings.Contains(err.Error(), "not allowed") {
		t.Fatalf("expected a protocol-not-allowed error, got: %v", err)
	}
	if _, statErr := os.Stat(marker); statErr == nil {
		t.Fatal("ext:: helper executed — RCE: marker file was created")
	}
}
