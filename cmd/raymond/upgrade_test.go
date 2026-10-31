package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/cbspire/raymond/internal/config"
	"github.com/cbspire/raymond/internal/control"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
)

func TestNativeUpgradeVersionArguments(t *testing.T) {
	for _, args := range [][]string{nil, {"--version", "0.1.2"}, {"--version", "1.2.3"}} {
		if _, e := upgradeVersion(args); e != nil {
			t.Fatal(e)
		}
	}
	for _, args := range [][]string{{"--version"}, {"--version", "main"}, {"--version", "https://example.com/a"}, {"--version", "0.1.2-rc.1"}, {"--version", "0.1.2", "extra"}, {"--url", "x"}} {
		if _, e := upgradeVersion(args); e == nil {
			t.Fatal("accepted", args)
		}
	}
}

type upgradeBackend struct {
	mu        sync.Mutex
	status    string
	shadow    bool
	completed bool
	requests  []control.Request
}

func (b *upgradeBackend) State(context.Context) (control.State, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.completed {
		return control.State{}, fmt.Errorf("dashboard requested before upgrade completion")
	}
	c := config.Config{DNS: config.DNS{Zones: []string{"test"}}}
	if b.shadow {
		c.Routes = []config.Route{{Domain: "raymond.test", Target: "127.0.0.1:3000"}}
	}
	return control.State{Version: "0.1.2", Running: true, Active: control.ConfigView{Config: c}}, nil
}
func (b *upgradeBackend) Submit(_ context.Context, r control.Request) (control.Operation, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.requests = append(b.requests, r)
	return control.Operation{ID: "upgrade-operation", Action: r.Action, Status: "queued"}, nil
}
func (b *upgradeBackend) GetOperation(string) (control.Operation, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.completed = true
	return control.Operation{ID: "upgrade-operation", Status: b.status, Error: "fixture update failed"}, nil
}
func (*upgradeBackend) Logs(context.Context, http.ResponseWriter, *http.Request) {}

func TestUpgradeAutomaticallyAuthorizesDashboard(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("dashboard launch files belong to the ordinary owner")
	}
	for _, test := range []struct {
		name, status                string
		browserError, shadow, opens bool
	}{
		{name: "successful update", status: "succeeded", opens: true},
		{name: "failed update", status: "failed"},
		{name: "browser unavailable", status: "succeeded", browserError: true, opens: true},
		{name: "application shadows dashboard", status: "succeeded", shadow: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			home := t.TempDir()
			if e := os.Chmod(home, 0700); e != nil {
				t.Fatal(e)
			}
			t.Setenv("HOME", home)
			dir, e := os.MkdirTemp("", "rup-")
			if e != nil {
				t.Fatal(e)
			}
			defer os.RemoveAll(dir)
			socket := filepath.Join(dir, "s.sock")
			listener, e := net.Listen("unix", socket)
			if e != nil {
				t.Fatal(e)
			}
			auth, e := control.NewAuth(filepath.Join(home, "sessions.json"))
			if e != nil {
				t.Fatal(e)
			}
			backend := &upgradeBackend{status: test.status, shadow: test.shadow}
			handler := control.Handler(backend, auth, "owner-secret", "proxy-secret")
			server := control.UnixServer(handler, "owner-secret", os.Geteuid())
			go func() { _ = server.Serve(listener) }()
			defer server.Close()
			client := control.NewClient(socket, "owner-secret")
			defer client.HTTP.CloseIdleConnections()
			opened := false
			opener := func(ctx context.Context, fileURL string) error {
				if opened {
					t.Fatal("dashboard opened more than once")
				}
				opened = true
				location, e := url.Parse(fileURL)
				if e != nil || location.Scheme != "file" || location.RawQuery != "" || location.Fragment != "" || location.Path != filepath.Join(home, ".raymond", "dashboard-launch.html") {
					t.Fatalf("unsafe browser launch: %v", e)
				}
				st, e := os.Stat(location.Path)
				if e != nil || st.Mode().Perm() != 0600 {
					t.Fatal("launch file must be private", e)
				}
				data, e := os.ReadFile(location.Path)
				if e != nil {
					t.Fatal(e)
				}
				match := regexp.MustCompile(`#token=([^"<>]+)`).FindSubmatch(data)
				if len(match) != 2 {
					t.Fatal("private launch file is missing authorization")
				}
				if test.browserError {
					return fmt.Errorf("desktop browser unavailable")
				}
				// The automatically opened file must contain a fresh, usable authorization,
				// rather than just navigating to an unauthenticated dashboard URL.
				proxy := control.NewClient(socket, "proxy-secret")
				defer proxy.HTTP.CloseIdleConnections()
				request, e := http.NewRequestWithContext(ctx, "POST", "http://control"+control.Prefix+"session", strings.NewReader(`{"token":"`+string(match[1])+`"}`))
				if e != nil {
					t.Fatal(e)
				}
				request.Host = "raymond.test"
				request.Header.Set("Authorization", "Bearer proxy-secret")
				request.Header.Set("Origin", control.Origin)
				request.Header.Set("Content-Type", "application/json")
				response, e := proxy.HTTP.Do(request)
				if e != nil {
					t.Fatal(e)
				}
				defer response.Body.Close()
				var session map[string]string
				if e = json.NewDecoder(response.Body).Decode(&session); e != nil {
					t.Fatal(e)
				}
				if response.StatusCode != 200 || session["session"] == "" || session["csrf"] == "" || len(response.Cookies()) != 0 {
					t.Fatal("upgrade did not authorize the dashboard")
				}
				return nil
			}
			e = upgradeManaged(client, "0.1.2", opener)
			if (e != nil) != (test.status == "failed") {
				t.Fatalf("wrong upgrade outcome: %v", e)
			}
			if opened != test.opens {
				t.Fatalf("dashboard opened=%v, expected %v", opened, test.opens)
			}
			backend.mu.Lock()
			defer backend.mu.Unlock()
			if len(backend.requests) != 1 || backend.requests[0].Action != "update" || backend.requests[0].Version != "0.1.2" {
				t.Fatal("upgrade was not submitted exactly once")
			}
			if !test.opens {
				if _, e = os.Stat(filepath.Join(home, ".raymond", "dashboard-launch.html")); !os.IsNotExist(e) {
					t.Fatal("failed upgrade or shadowed dashboard created a launch file", e)
				}
			}
		})
	}
}
