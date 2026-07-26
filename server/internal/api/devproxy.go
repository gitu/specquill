package api

import (
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
)

// devViteProxy reverse-proxies the SPA routes to the vite dev server so that
// in -dev mode :8643 serves current frontend source (HMR websocket included —
// vite's client dials location.port, which lands back here and is upgraded
// through the proxy). When vite is not running (`make dev-server`, e2e) every
// request falls through to the embedded build, so the proxy is safe to have
// always-on in dev.
func devViteProxy(fallback http.Handler) http.Handler {
	proxy := httputil.NewSingleHostReverseProxy(devViteTarget(os.Getenv("SPECQUILL_VITE_ADDR")))
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, _ error) {
		fallback.ServeHTTP(w, r)
	}
	return proxy
}

// devViteTarget accepts SPECQUILL_VITE_ADDR as either host:port or a full
// http(s) URL; anything unreachable just means every request takes the
// embedded-build fallback.
func devViteTarget(v string) *url.URL {
	if v == "" {
		return &url.URL{Scheme: "http", Host: "127.0.0.1:5643"}
	}
	if u, err := url.Parse(v); err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != "" {
		return &url.URL{Scheme: u.Scheme, Host: u.Host}
	}
	return &url.URL{Scheme: "http", Host: v}
}
