package api

import (
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"specquill/server/internal/auth"
	"specquill/server/internal/config"
	"specquill/server/internal/gitx"
	"specquill/server/internal/store"
)

// Link verification: internal links (relative, root-absolute, tolerant bare)
// resolve against the branch; ~source links need a grant; externals are
// skipped when disabled and private addresses are refused, never probed.
func TestLinkCheck(t *testing.T) {
	tmp := t.TempDir()
	src := filepath.Join(tmp, "src")
	run := func(args ...string) {
		if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	run("init", "-b", "main", src)
	write := func(rel, content string) {
		abs := filepath.Join(src, filepath.FromSlash(rel))
		_ = os.MkdirAll(filepath.Dir(abs), 0o755)
		if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("index.md", "# Index\n\n- [ok relative](requirements/REQ-001.md)\n- [broken absolute](/requirements/MISSING.md)\n")
	write("specs/a.md", "---\ntype: Specification\n---\n\nbody\n")
	write("requirements/REQ-001.md", strings.Join([]string{
		"---", "type: Requirement", "---", "",
		"[up-relative ok](../specs/a.md)",
		"[sibling broken](REQ-002.md)",
		"[ungranted source](~regulations/regulations/mifid-ii.md)",
		"[private ext](http://127.0.0.1:1/x)",
		"[mailto ignored](mailto:x@example.com)",
		"[anchor ignored](#section)",
		"```", "[fenced ignored](nowhere.md)", "```", "",
	}, "\n"))
	run("-C", src, "add", "-A")
	run("-C", src, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-qm", "init")

	cfg := &config.Config{
		Tenant:   &config.TenantConfig{Slug: "default", DisplayName: "Workspace", DefaultRole: "editor"},
		DataDir:  filepath.Join(tmp, "data"),
		Git:      config.GitConfig{CommitterName: "svc", CommitterEmail: "svc@t"},
		Session:  config.SessionConfig{TTL: time.Hour, CookieSecure: false},
		Auth:     config.AuthConfig{Local: config.LocalAuthConfig{Enabled: true}},
		Projects: []config.ProjectConfig{{ID: "r", Remote: src, DefaultBranch: "main", ProtectedBranches: []string{"_none"}}},
	}
	cfg.Normalize()
	st := store.OpenTest(t)
	ten, err := st.EnsureTenant("default", "config", 0, "Workspace")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SyncTenantProjects(ten.ID, []store.Project{{ProjectID: "r", RepoID: "r"}}); err != nil {
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
		Dist:     fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("<html></html>")}},
	})
	cookie := login(t, h)

	kindCounts := func(out map[string]any, kind string) map[string]any {
		return out["counts"].(map[string]any)[kind].(map[string]any)
	}
	num := func(m map[string]any, k string) int {
		if v, ok := m[k].(float64); ok {
			return int(v)
		}
		return 0
	}
	problemSet := func(out map[string]any) map[string]string {
		set := map[string]string{}
		if ps, ok := out["problems"].([]any); ok {
			for _, p := range ps {
				m := p.(map[string]any)
				set[m["href"].(string)], _ = m["detail"].(string)
			}
		}
		return set
	}

	// externals disabled: internal + source verified, external counted skipped
	code, out := doJSON(t, h, cookie, "GET", "/api/repos/r/linkcheck?external=0", nil)
	if code != http.StatusOK {
		t.Fatalf("linkcheck: %d %v", code, out)
	}
	in := kindCounts(out, "internal")
	if num(in, "ok") != 2 || num(in, "broken") != 2 {
		t.Fatalf("internal counts: %v", in)
	}
	if srcC := kindCounts(out, "source"); num(srcC, "broken") != 1 {
		t.Fatalf("source counts: %v", srcC)
	}
	if ext := kindCounts(out, "external"); num(ext, "skipped") != 1 || num(ext, "broken") != 0 {
		t.Fatalf("external counts: %v", ext)
	}
	problems := problemSet(out)
	if _, ok := problems["/requirements/MISSING.md"]; !ok {
		t.Fatalf("missing absolute-link problem: %v", problems)
	}
	if _, ok := problems["REQ-002.md"]; !ok {
		t.Fatalf("missing sibling-link problem: %v", problems)
	}
	if d := problems["~regulations/regulations/mifid-ii.md"]; !strings.Contains(d, "not granted") {
		t.Fatalf("source problem detail: %q (%v)", d, problems)
	}
	if _, ok := problems["mailto:x@example.com"]; ok {
		t.Fatalf("mailto reported: %v", problems)
	}
	for href := range problems {
		if strings.Contains(href, "nowhere.md") {
			t.Fatalf("fenced link reported: %v", problems)
		}
	}

	// externals enabled: the loopback URL is refused by the SSRF guard, not probed
	code, out = doJSON(t, h, cookie, "GET", "/api/repos/r/linkcheck", nil)
	if code != http.StatusOK {
		t.Fatalf("linkcheck external: %d", code)
	}
	if ext := kindCounts(out, "external"); num(ext, "skipped") != 1 || num(ext, "ok") != 0 {
		t.Fatalf("external counts with probing: %v", ext)
	}
}
