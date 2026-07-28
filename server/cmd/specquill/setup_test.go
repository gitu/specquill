package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"specquill/server/internal/config"
)

// answers joins one wizard input line per prompt.
func answers(lines ...string) *strings.Reader {
	return strings.NewReader(strings.Join(lines, "\n") + "\n")
}

func TestSetupForgeMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "specquill.yml")
	in := answers(
		"",                          // listen → :8080
		"https://specs.example.com", // base URL
		"",                          // data dir → ./data
		"git@gitlab.example.com:acme/trading-specs.git", // remote
		"",                                 // project id → trading-specs
		"",                                 // branch → main
		"",                                 // content root → repo root
		"forge",                            // auth mode
		"gitlab",                           // forge kind
		"https://gitlab.example.com",       // self-hosted base
		"flo@example.com, ops@example.com", // admins
		"",                                 // committer name → specquill
		"",                                 // committer email
		"n",                                // copilot off
	)
	var out strings.Builder
	if err := runSetup(in, &out, path); err != nil {
		t.Fatalf("runSetup: %v\noutput:\n%s", err, out.String())
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("generated config does not load: %v", err)
	}
	if cfg.Projects[0].ID != "trading-specs" {
		t.Errorf("project id = %q, want trading-specs (derived from remote)", cfg.Projects[0].ID)
	}
	if cfg.Auth.Forge.Kind != "gitlab" || cfg.Auth.Forge.BaseURL != "https://gitlab.example.com" {
		t.Errorf("forge auth = %+v", cfg.Auth.Forge)
	}
	if len(cfg.Auth.AdminEmails) != 2 || cfg.Auth.AdminEmails[0] != "flo@example.com" {
		t.Errorf("admin emails = %v", cfg.Auth.AdminEmails)
	}
	if !cfg.Session.CookieSecure {
		t.Error("https base URL should imply cookie_secure: true")
	}
	if cfg.AI.Enabled {
		t.Error("copilot should be off")
	}
}

func TestSetupLocalModeWithAI(t *testing.T) {
	path := filepath.Join(t.TempDir(), "specquill.yml")
	in := answers(
		":9090",                            // listen
		"",                                 // base URL → http://localhost:9090
		"/var/lib/specquill",               // data dir
		"https://github.com/acme/reqs.git", // remote
		"reqs",                             // project id
		"trunk",                            // branch
		"docs/specs",                       // content root
		"local",                            // auth mode
		"",                                 // token env → SPECQUILL_TOKEN
		"",                                 // admins → none
		"svc",                              // committer name
		"svc@acme.io",                      // committer email
		"y",                                // copilot on
		"",                                 // base URL → openai
		"",                                 // model → gpt-4o
		"",                                 // quick model
		"",                                 // key env → SPECQUILL_AI_KEY
	)
	var out strings.Builder
	if err := runSetup(in, &out, path); err != nil {
		t.Fatalf("runSetup: %v\noutput:\n%s", err, out.String())
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("generated config does not load: %v", err)
	}
	p := cfg.Projects[0]
	if p.ID != "reqs" || p.DefaultBranch != "trunk" || p.ContentRoot != "docs/specs" || p.TokenEnv != "SPECQUILL_TOKEN" {
		t.Errorf("project = %+v", p)
	}
	if !cfg.Auth.Local.Enabled || cfg.Auth.Forge.Enabled() {
		t.Errorf("auth = %+v", cfg.Auth)
	}
	if cfg.Session.CookieSecure {
		t.Error("http base URL should imply cookie_secure: false")
	}
	if !cfg.AI.Enabled || cfg.AI.Model != "gpt-4o" || cfg.AI.APIKeyEnv != "SPECQUILL_AI_KEY" {
		t.Errorf("ai = %+v", cfg.AI)
	}
	// the wizard's local-mode next steps mention user creation
	if !strings.Contains(out.String(), "user add") {
		t.Error("local mode should point at `specquill user add`")
	}
}

func TestSetupRefusesSilentOverwrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "specquill.yml")
	if err := os.WriteFile(path, []byte("listen: :1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	in := answers("n") // decline the overwrite
	var out strings.Builder
	if err := runSetup(in, &out, path); err == nil {
		t.Fatal("expected abort when declining overwrite")
	}
	raw, _ := os.ReadFile(path)
	if string(raw) != "listen: :1\n" {
		t.Error("declining the overwrite must leave the file untouched")
	}
}

func TestDefaultBaseURL(t *testing.T) {
	cases := map[string]string{
		":8080":        "http://localhost:8080",
		"0.0.0.0:9000": "http://localhost:9000",
		"[::]:9000":    "http://localhost:9000",
		"10.1.2.3:80":  "http://10.1.2.3:80",
		"garbage":      "http://localhost:8080",
	}
	for in, want := range cases {
		if got := defaultBaseURL(in); got != want {
			t.Errorf("defaultBaseURL(%q) = %q, want %q", in, got, want)
		}
	}
}
