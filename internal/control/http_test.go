package control

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

type stubBackend struct{ calls int }

func (s *stubBackend) State(context.Context) (State, error) { return State{Version: "test"}, nil }
func (s *stubBackend) Submit(context.Context, Request) (Operation, error) {
	s.calls++
	return Operation{ID: "test", Status: "queued"}, nil
}
func (s *stubBackend) GetOperation(string) (Operation, error)                   { return Operation{}, nil }
func (s *stubBackend) Logs(context.Context, http.ResponseWriter, *http.Request) {}
func TestBrowserBoundary(t *testing.T) {
	a, e := NewAuth(filepath.Join(t.TempDir(), "auth.json"))
	if e != nil {
		t.Fatal(e)
	}
	launch, _ := a.Launch()
	cookie, csrf, e := a.Exchange(launch)
	if e != nil {
		t.Fatal(e)
	}
	if _, _, e = a.Exchange(launch); e == nil {
		t.Fatal("launch token reused")
	}
	b := &stubBackend{}
	h := Handler(b, a, "owner", "proxy")
	for _, tc := range []struct {
		name, host, origin, csrf, cookie, auth string
		status                                 int
	}{
		{"allowed", "raymond.test", Origin, csrf, cookie, "proxy", 202},
		{"no session", "raymond.test", Origin, csrf, "", "proxy", 401},
		{"csrf", "raymond.test", Origin, "bad", cookie, "proxy", 403},
		{"site", "raymond.test", "http://app.raymond.test", csrf, cookie, "proxy", 403},
		{"host", "app.raymond.test", Origin, csrf, cookie, "proxy", 403},
		{"no origin", "raymond.test", "", csrf, cookie, "proxy", 403},
		{"no broker credential", "raymond.test", Origin, csrf, cookie, "bad", 401},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest("POST", "http://"+tc.host+Prefix+"operations", strings.NewReader(`{"action":"restart"}`))
			r.Header.Set("Authorization", "Bearer "+tc.auth)
			r.Header.Set("Origin", tc.origin)
			r.Header.Set("X-Raymond-CSRF", tc.csrf)
			r.Header.Set("Content-Type", "application/json")
			r.AddCookie(&http.Cookie{Name: CookieName, Value: tc.cookie})
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != tc.status {
				t.Fatalf("%d %s", w.Code, w.Body.String())
			}
		})
	}
	if b.calls != 1 {
		t.Fatal("unauthorized operation executed")
	}
	saved, e := NewAuth(a.path)
	if e != nil {
		t.Fatal(e)
	}
	if _, ok := saved.Session(cookie); !ok {
		t.Fatal("session lost on restart")
	}
}
func TestBrowserCannotMintLaunch(t *testing.T) {
	a, _ := NewAuth(filepath.Join(t.TempDir(), "auth.json"))
	h := Handler(&stubBackend{}, a, "owner", "proxy")
	r := httptest.NewRequest("POST", Origin+Prefix+"launch", nil)
	r.Header.Set("Authorization", "Bearer proxy")
	r.Header.Set("Origin", Origin)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal(w.Code)
	}
}
