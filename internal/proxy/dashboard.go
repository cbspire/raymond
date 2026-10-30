package proxy

import (
	"bytes"
	"crypto/rand"
	_ "embed"
	"github.com/cbspire/raymond/internal/control"
	"net/http"
	"strings"
)

//go:embed web/index.html
var dashboardPage []byte

//go:embed web/dashboard.css
var dashboardCSS []byte

//go:embed web/dashboard.js
var dashboardJS []byte

//go:embed web/theme.js
var dashboardTheme []byte

func serveManagement(w http.ResponseWriter, r *http.Request, client *control.Client) {
	if strings.HasPrefix(r.URL.Path, control.Prefix) {
		if client == nil {
			control.JSON(w, http.StatusServiceUnavailable, map[string]string{"error": "Local management is not enrolled. The manager backend must be installed before dashboard controls can be used."})
			return
		}
		client.Forward(w, r)
		return
	}
	serveDashboard(w, r)
}

func serveDashboard(w http.ResponseWriter, r *http.Request) {
	content, mime := dashboardPage, "text/html; charset=utf-8"
	switch r.URL.Path {
	case "/":
	case "/dashboard.css":
		content, mime = dashboardCSS, "text/css; charset=utf-8"
	case "/theme.js":
		content, mime = dashboardTheme, "text/javascript; charset=utf-8"
	case "/dashboard.js":
		content, mime = dashboardJS, "text/javascript; charset=utf-8"
	default:
		http.NotFound(w, r)
		return
	}

	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "Raymond: method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", mime)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	styleSource := "'self'"
	if r.URL.Path == "/" {
		nonce := rand.Text()
		content = bytes.ReplaceAll(content, []byte("__RAYMOND_CSP_NONCE__"), []byte(nonce))
		styleSource += " 'nonce-" + nonce + "'"
	}
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src "+styleSource+"; script-src 'self'; connect-src 'self'; img-src data:; base-uri 'none'; frame-ancestors 'none'; form-action 'none'")
	if r.Method != http.MethodHead {
		_, _ = w.Write(content)
	}
}
