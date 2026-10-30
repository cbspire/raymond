package control

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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
	session, csrf, e := a.Exchange(launch)
	if e != nil {
		t.Fatal(e)
	}
	if _, _, e = a.Exchange(launch); e == nil {
		t.Fatal("launch token reused")
	}
	b := &stubBackend{}
	h := Handler(b, a, "owner", "proxy")
	for _, tc := range []struct {
		name, host, origin, csrf, session, auth string
		status                                  int
	}{
		{"allowed", "raymond.test", Origin, csrf, session, "proxy", 202},
		{"no session", "raymond.test", Origin, csrf, "", "proxy", 401},
		{"csrf", "raymond.test", Origin, "bad", session, "proxy", 403},
		{"site", "raymond.test", "http://app.raymond.test", csrf, session, "proxy", 403},
		{"host", "app.raymond.test", Origin, csrf, session, "proxy", 403},
		{"no origin", "raymond.test", "", csrf, session, "proxy", 403},
		{"no broker credential", "raymond.test", Origin, csrf, session, "bad", 401},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest("POST", "http://"+tc.host+Prefix+"operations", strings.NewReader(`{"action":"restart"}`))
			r.Header.Set("Authorization", "Bearer "+tc.auth)
			r.Header.Set("Origin", tc.origin)
			r.Header.Set("X-Raymond-CSRF", tc.csrf)
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set(SessionHeader, tc.session)
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
	if _, ok := saved.Session(session); !ok {
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

func TestOperationRequestBoundary(t *testing.T) {
	a, e := NewAuth(filepath.Join(t.TempDir(), "auth.json"))
	if e != nil {
		t.Fatal(e)
	}
	b := &stubBackend{}
	h := Handler(b, a, "owner", "proxy")
	for _, body := range []string{
		`{"action":"exec"}`, `{"action":"restart","path":"/etc/passwd"}`,
		`{"action":"update","version":"https://attacker.invalid/payload"}`,
		`{"action":"restart","revision":"unexpected"}`, `{"action":"save"}`,
		`{"action":"restart"} {"action":"stop"}`,
	} {
		r := httptest.NewRequest("POST", "http://control"+Prefix+"operations", strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer owner")
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 400 {
			t.Fatalf("accepted %s: %d", body, w.Code)
		}
	}
	if b.calls != 0 {
		t.Fatal("invalid request reached backend")
	}
}

func TestBrowserSessionUsesExplicitHeader(t *testing.T) {
	auth, err := NewAuth(filepath.Join(t.TempDir(), "sessions.json"))
	if err != nil {
		t.Fatal(err)
	}
	launch, err := auth.Launch()
	if err != nil {
		t.Fatal(err)
	}
	handler := Handler(&stubBackend{}, auth, "owner", "proxy")
	request := httptest.NewRequest("POST", Origin+Prefix+"session", strings.NewReader(`{"token":"`+launch+`"}`))
	request.Header.Set("Authorization", "Bearer proxy")
	request.Header.Set("Origin", Origin)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	var session map[string]string
	if err = json.Unmarshal(response.Body.Bytes(), &session); err != nil {
		t.Fatal(err)
	}
	if response.Code != 200 || response.Header().Get("Set-Cookie") != "" || session["session"] == "" || session["csrf"] == "" {
		t.Fatal("browser exchange must return a header session without cookies")
	}
	for _, test := range []struct {
		name    string
		headers http.Header
		status  int
	}{
		{"header", http.Header{"X-Raymond-Session": {session["session"]}}, 200},
		{"duplicate", http.Header{"X-Raymond-Session": {session["session"], session["session"]}}, 401},
		{"cookie only", http.Header{"Cookie": {"raymond_session=" + session["session"]}}, 401},
		{"cookie ambiguity", http.Header{"Cookie": {"raymond_session=" + session["session"] + "; raymond_session=other"}}, 401},
		{"missing", http.Header{}, 401},
		{"invalid", http.Header{"X-Raymond-Session": {"invalid"}}, 401},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest("GET", Origin+Prefix+"state", nil)
			request.Header = test.headers
			request.Header.Set("Authorization", "Bearer proxy")
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != test.status {
				t.Fatalf("status %d, expected %d", response.Code, test.status)
			}
		})
	}
}

func TestHeaderSessionMigrationRevokesLegacyCookies(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.json")
	legacy := map[string]any{"sessions": map[string]any{hash("legacy-cookie"): session{CSRF: "old-csrf", Expires: time.Now().Add(time.Hour)}}, "launches": map[string]time.Time{hash("legacy-launch"): time.Now().Add(time.Minute)}}
	if err := AtomicJSON(path, legacy); err != nil {
		t.Fatal(err)
	}
	auth, err := NewAuth(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := auth.Session("legacy-cookie"); ok {
		t.Fatal("legacy cookie remained a valid header capability")
	}
	if _, _, err := auth.Exchange("legacy-launch"); err == nil {
		t.Fatal("legacy launch survived authentication migration")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "old-csrf") || strings.Contains(string(data), hash("legacy-cookie")) {
		t.Fatal("legacy session survived on disk")
	}
	reopened, err := NewAuth(path)
	if err != nil {
		t.Fatal(err)
	}
	token, err := reopened.Launch()
	if err != nil {
		t.Fatal(err)
	}
	capability, _, err := reopened.Exchange(token)
	if err != nil {
		t.Fatal(err)
	}
	persisted, err := NewAuth(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := persisted.Session(capability); !ok {
		t.Fatal("header capability was lost on restart")
	}
}
