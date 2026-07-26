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
	target := &url.URL{Scheme: "http", Host: "127.0.0.1:5643"}
	if v := os.Getenv("SPECQUILL_VITE_ADDR"); v != "" {
		target.Host = v
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, _ error) {
		fallback.ServeHTTP(w, r)
	}
	return proxy
}
