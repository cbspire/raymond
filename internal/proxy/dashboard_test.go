package proxy

import (
	"encoding/json"
	"fmt"
	"github.com/cbspire/raymond/internal/control"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cbspire/raymond/internal/config"
	"github.com/cbspire/raymond/internal/router"
)

func TestDashboardPage(t *testing.T) {
	h := New(router.New(nil))
	for _, tc := range []struct {
		host, method, path string
		status             int
	}{
		{"raymond.test", "GET", "/", 200},
		{"RAYMOND.TEST.:8080", "GET", "/", 200},
		{"raymond.test", "HEAD", "/", 200},
		{"raymond.test", "POST", "/", 405},
		{"raymond.test", "GET", "/missing", 404},
		{"unknown.raymond.test", "GET", "/", 404},
		{"evilraymond.test", "GET", "/", 404},
	} {
		t.Run(tc.host+tc.method+tc.path, func(t *testing.T) {
			r := httptest.NewRequest(tc.method, "http://"+tc.host+tc.path, nil)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != tc.status {
				t.Fatalf("got HTTP %d, want %d", w.Code, tc.status)
			}
			if tc.status != 200 {
				return
			}
			if w.Header().Get("Content-Type") != "text/html; charset=utf-8" {
				t.Fatal(w.Header())
			}
			if tc.method == "HEAD" {
				if w.Body.Len() != 0 {
					t.Fatal("HEAD returned a body")
				}
			} else if !strings.Contains(w.Body.String(), "Your local services") {
				t.Fatal("missing dashboard page")
			}
		})
	}
}

func TestExplicitRouteOverridesDashboard(t *testing.T) {
	b := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "my own app")
	}))
	defer b.Close()
	h := New(router.New([]config.Route{{Domain: "raymond.test", Target: strings.TrimPrefix(b.URL, "http://")}}))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "http://raymond.test/", nil))
	if w.Code != 200 || w.Body.String() != "my own app" {
		t.Fatalf("dashboard shadowed configured app: %d %q", w.Code, w.Body.String())
	}
}

func TestDashboardAssetsAndMissingManager(t *testing.T) {
	h := newWithControl(router.New(nil), nil)
	for path, mime := range map[string]string{"/dashboard.js": "text/javascript", "/dashboard.css": "text/css"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", "http://raymond.test"+path, nil))
		if w.Code != 200 || !strings.HasPrefix(w.Header().Get("Content-Type"), mime) {
			t.Fatal(w.Code, w.Header())
		}
		csp := w.Header().Get("Content-Security-Policy")
		if !strings.Contains(csp, "script-src 'self'") || strings.Contains(csp, "unsafe-inline") {
			t.Fatal(csp)
		}
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "http://raymond.test/api/v1/state", nil))
	if w.Code != 503 || !strings.Contains(w.Body.String(), "not enrolled") {
		t.Fatal(w.Code, w.Body.String())
	}
	for _, path := range []string{"/dashboard.js", "/dashboard.css", "/api/v1/state"} {
		w = httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", "http://app.test"+path, nil))
		if w.Code != 404 {
			t.Fatal("management exposed on app hostname", w.Code)
		}
	}
}

func TestDashboardForwardsOnlyWithProxyCredential(t *testing.T) {
	socket := filepath.Join(t.TempDir(), "control.sock")
	listener, e := net.Listen("unix", socket)
	if e != nil {
		t.Fatal(e)
	}
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer proxy-secret" || r.Host != "raymond.test" {
			t.Error("incorrect forwarded identity")
		}
		control.JSON(w, 200, map[string]string{"result": "forwarded"})
	}))
	server.Listener = listener
	server.Start()
	defer server.Close()
	client := control.NewClient(socket, "proxy-secret")
	defer client.HTTP.CloseIdleConnections()
	h := newWithControl(router.New(nil), client)
	r := httptest.NewRequest("GET", "http://raymond.test/api/v1/state", nil)
	r.Header.Set("Authorization", "Bearer owner-secret")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	var result map[string]string
	if e = json.Unmarshal(w.Body.Bytes(), &result); e != nil || result["result"] != "forwarded" {
		t.Fatal(e, w.Body.String())
	}
	// A configured application owns all its paths, including dashboard assets/API.
	app := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "application") }))
	defer app.Close()
	h = newWithControl(router.New([]config.Route{{Domain: "raymond.test", Target: strings.TrimPrefix(app.URL, "http://")}}), client)
	for _, path := range []string{"/", "/dashboard.js", "/api/v1/launch"} {
		w = httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", "http://raymond.test"+path, nil))
		if w.Body.String() != "application" {
			t.Fatal("management intercepted app route", path)
		}
	}
}
