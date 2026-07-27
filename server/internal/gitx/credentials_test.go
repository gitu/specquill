package gitx

import (
	"strings"
	"testing"

	"specquill/server/internal/config"
)

// envOf extracts a KEY=value from a credential env slice.
func envOf(env []string, key string) (string, bool) {
	for _, e := range env {
		if v, ok := strings.CutPrefix(e, key+"="); ok {
			return v, true
		}
	}
	return "", false
}

// The credential seam takes the token PER CALL: the passed value wins, an
// empty one falls back to token_env, and neither is ever stored on the repo —
// so two concurrent callers cannot see each other's token.
func TestCredentialArgsUsesPassedToken(t *testing.T) {
	r := &Repo{Cfg: config.RepoConfig{
		Remote: "https://git.example.com/acme/specs.git", TokenEnv: "SPECQUILL_TEST_TOKEN",
	}}
	t.Setenv("SPECQUILL_TEST_TOKEN", "from-env")

	// explicit token wins over token_env
	args, env := r.credentialArgs("from-caller")
	if len(args) == 0 {
		t.Fatal("expected credential helper args")
	}
	if got, _ := envOf(env, "SPECQUILL_GIT_TOKEN"); got != "from-caller" {
		t.Fatalf("token: got %q, want the caller's", got)
	}
	// host-scoped to the repo's own remote
	if got, _ := envOf(env, "SPECQUILL_GIT_HOST"); got != "git.example.com" {
		t.Fatalf("host scope: got %q", got)
	}
	// no leakage between calls: a second call with a different token gets it
	if _, env2 := r.credentialArgs("other"); func() string { v, _ := envOf(env2, "SPECQUILL_GIT_TOKEN"); return v }() != "other" {
		t.Fatal("second call must use its own token")
	}
	if got, _ := envOf(env, "SPECQUILL_GIT_TOKEN"); got != "from-caller" {
		t.Fatalf("first call's env was mutated: %q", got)
	}

	// empty token → token_env fallback (local/dev mode)
	if _, env := r.credentialArgs(""); func() string { v, _ := envOf(env, "SPECQUILL_GIT_TOKEN"); return v }() != "from-env" {
		t.Fatal("empty token must fall back to token_env")
	}

	// no token anywhere → no helper at all
	bare := &Repo{Cfg: config.RepoConfig{Remote: "https://git.example.com/x.git"}}
	if args, env := bare.credentialArgs(""); args != nil || env != nil {
		t.Fatalf("uncredentialed repo: got %v %v", args, env)
	}
}

func TestRemoteHostScoping(t *testing.T) {
	cases := map[string]string{
		"https://git.example.com/acme/specs.git": "git.example.com",
		"http://127.0.0.1:8443/x.git":            "127.0.0.1:8443",
		"https://GIT.Example.COM/x.git":          "git.example.com",
		"git@github.com:acme/specs.git":          "", // ssh: token never used
		"/srv/git/specs.git":                     "",
	}
	for remote, want := range cases {
		if got := remoteHost(remote); got != want {
			t.Errorf("%s → %q, want %q", remote, got, want)
		}
	}
}
