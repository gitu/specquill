package api

import (
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestDevViteTarget(t *testing.T) {
	cases := []struct {
		in   string
		want url.URL
	}{
		{"", url.URL{Scheme: "http", Host: "127.0.0.1:5643"}},
		{"127.0.0.1:9999", url.URL{Scheme: "http", Host: "127.0.0.1:9999"}},
		{"http://127.0.0.1:9999", url.URL{Scheme: "http", Host: "127.0.0.1:9999"}},
		{"https://vite.dev.internal:5643", url.URL{Scheme: "https", Host: "vite.dev.internal:5643"}},
	}
	for _, c := range cases {
		if got := devViteTarget(c.in); *got != c.want {
			t.Errorf("devViteTarget(%q) = %v, want %v", c.in, got, &c.want)
		}
	}
}

func TestDevViteProxyForwards(t *testing.T) {
	vite := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "from vite")
	}))
	defer vite.Close()
	t.Setenv("SPECQUILL_VITE_ADDR", vite.URL)

	fallback := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("fallback served while vite is up")
	})
	rec := httptest.NewRecorder()
	devViteProxy(fallback).ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if rec.Code != http.StatusOK || rec.Body.String() != "from vite" {
		t.Fatalf("proxied response: %d %q", rec.Code, rec.Body.String())
	}
}

func TestDevViteProxyFallsBackWhenViteDown(t *testing.T) {
	// Grab a port nothing listens on so the dial fails immediately.
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	l.Close()
	t.Setenv("SPECQUILL_VITE_ADDR", addr)

	fallback := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "embedded build")
	})
	rec := httptest.NewRecorder()
	devViteProxy(fallback).ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "embedded build") {
		t.Fatalf("fallback response: %d %q", rec.Code, rec.Body.String())
	}
}
