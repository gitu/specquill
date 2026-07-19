package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"specquill/server/internal/config"
	"specquill/server/internal/store"
)

func TestHostTenantResolution(t *testing.T) {
	st := store.OpenTest(t)
	if _, err := st.EnsureTenant("acme", "config", 0, "Acme"); err != nil {
		t.Fatal(err)
	}
	s := &Server{cfg: &config.Config{BaseDomain: "specquill.app"}, store: st}

	req := func(host, xfh string, proxy bool) *http.Request {
		r := httptest.NewRequest("GET", "/api/me", nil)
		r.Host = host
		if xfh != "" {
			r.Header.Set("X-Forwarded-Host", xfh)
		}
		s.cfg.TrustedProxy = proxy
		return r
	}
	slugOf := func(r *http.Request) string {
		if tt := s.hostTenant(r); tt != nil {
			return tt.Slug
		}
		return ""
	}

	if got := slugOf(req("acme.specquill.app", "", false)); got != "acme" {
		t.Errorf("subdomain: got %q want acme", got)
	}
	if got := slugOf(req("acme.specquill.app:8643", "", false)); got != "acme" {
		t.Errorf("subdomain with port: got %q want acme", got)
	}
	if got := slugOf(req("specquill.app", "", false)); got != "" {
		t.Errorf("apex should not pin a tenant: got %q", got)
	}
	if got := slugOf(req("unknown.specquill.app", "", false)); got != "" {
		t.Errorf("unknown slug should be nil: got %q", got)
	}
	if got := slugOf(req("deep.acme.specquill.app", "", false)); got != "" {
		t.Errorf("deeper subdomain should be nil: got %q", got)
	}
	if got := slugOf(req("acme.example.com", "", false)); got != "" {
		t.Errorf("other base domain should be nil: got %q", got)
	}
	// trusted proxy honors X-Forwarded-Host; untrusted ignores it
	if got := slugOf(req("proxy.internal", "acme.specquill.app", true)); got != "acme" {
		t.Errorf("trusted proxy XFH: got %q want acme", got)
	}
	if got := slugOf(req("proxy.internal", "acme.specquill.app", false)); got != "" {
		t.Errorf("untrusted proxy must ignore XFH: got %q", got)
	}

	// base_domain unset ⇒ never pins a tenant (self-host default)
	s2 := &Server{cfg: &config.Config{}, store: st}
	r := httptest.NewRequest("GET", "/api/me", nil)
	r.Host = "acme.specquill.app"
	if tt := s2.hostTenant(r); tt != nil {
		t.Errorf("base_domain unset should never resolve a host tenant, got %q", tt.Slug)
	}
}

// A session minted on one tenant's host must be rejected on another tenant's
// host (defense in depth beyond the host-only cookie).
func TestSessionHostBinding(t *testing.T) {
	h, st, _ := testServerCfg(t, false, func(c *config.Config) { c.BaseDomain = "specquill.app" })
	// a second tenant reachable by subdomain (the login user need not be a
	// member — the binding check runs before membership resolution)
	if _, err := st.EnsureTenant("beta", "config", 0, "Beta"); err != nil {
		t.Fatal(err)
	}

	// log in on the default tenant's host → cookie bound to "default"
	body, _ := json.Marshal(map[string]string{"username": "flo", "password": "hunter2secret"})
	lr := httptest.NewRequest("POST", "/auth/local/login", bytes.NewReader(body))
	lr.Host = "default.specquill.app"
	lr.Header.Set("X-SpecQuill", "1")
	lrec := httptest.NewRecorder()
	h.ServeHTTP(lrec, lr)
	var cookie *http.Cookie
	for _, c := range lrec.Result().Cookies() {
		if c.Name == "specquill_session" {
			cookie = c
		}
	}
	if cookie == nil {
		t.Fatal("no session cookie from login")
	}

	me := func(host string) int {
		r := httptest.NewRequest("GET", "/api/me", nil)
		r.Host = host
		r.AddCookie(cookie)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		return rec.Code
	}
	if code := me("default.specquill.app"); code != http.StatusOK {
		t.Fatalf("same-tenant host: want 200, got %d", code)
	}
	if code := me("beta.specquill.app"); code != http.StatusUnauthorized {
		t.Fatalf("cross-tenant host: want 401, got %d", code)
	}
}
