package proxy

import (
	"fmt"
	"net/http"
	"net/http/httptest"
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
			} else if !strings.Contains(w.Body.String(), "At your service.") {
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
