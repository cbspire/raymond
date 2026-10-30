package manager

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cbspire/raymond/internal/config"
	"github.com/cbspire/raymond/internal/control"
)

func backendFixture(t *testing.T) *Backend {
	t.Helper()
	data := []byte("# Keep this note\nversion: 1\ndns:\n  zones: [test]\nroutes:\n  - domain: studio.test\n    target: 127.0.0.1:3000\n")
	active, e := configView(data)
	if e != nil {
		t.Fatal(e)
	}
	var mu sync.Mutex
	b := &Backend{Directory: t.TempDir(), Version: "0.1.2", Metadata: control.Metadata{ConfigPath: "/owner/config.yaml"}, streams: make(chan struct{}, 4)}
	if e := os.Chmod(b.Directory, 0700); e != nil {
		t.Fatal(e)
	}
	b.Owner = func(ctx context.Context, r OwnerRequest) (OwnerResult, error) {
		mu.Lock()
		defer mu.Unlock()
		switch r.Action {
		case "config-read":
			return OwnerResult{Data: append([]byte(nil), data...), Revision: config.Revision(data)}, nil
		case "config-save":
			if r.Revision != config.Revision(data) {
				return OwnerResult{}, control.Conflict("stale edit")
			}
			next, e := config.WithComments(data, *r.Config)
			if e != nil {
				return OwnerResult{}, e
			}
			data = next
			return OwnerResult{Revision: config.Revision(data)}, nil
		default:
			return OwnerResult{}, fmt.Errorf("unsupported fixture action")
		}
	}
	b.Active = func() (control.ConfigView, error) { return active, nil }
	b.Running = func() bool { return true }
	b.Spawn = func(*control.Operations) error { return nil }
	return b
}
func TestBrokerStateAndDurableDispatch(t *testing.T) {
	b := backendFixture(t)
	ctx := context.Background()
	state, e := b.State(ctx)
	if e != nil || state.Pending || state.Saved.Revision != state.Active.Revision {
		t.Fatalf("%+v %v", state, e)
	}
	changed := state.Saved.Config
	changed.Routes = append([]config.Route(nil), changed.Routes...)
	changed.Routes[0].Target = "127.0.0.1:3001"
	op, e := b.Submit(ctx, control.Request{Action: "save", Revision: state.Saved.Revision, Config: &changed})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = b.Submit(ctx, control.Request{Action: "restart"}); e == nil {
		t.Fatal("accepted conflicting operation")
	}
	persisted, e := b.GetOperation(op.ID)
	if e != nil || persisted.Request.Config.Routes[0].Target != "127.0.0.1:3001" {
		t.Fatal(persisted, e)
	}
	j, e := control.OpenOperations(b.Directory)
	if e != nil {
		t.Fatal(e)
	}
	defer j.Close()
	if e = j.Start(op.ID); e != nil {
		t.Fatal(e)
	}
	if _, e = b.Owner(ctx, OwnerRequest{Action: "config-save", Revision: state.Saved.Revision, Config: &changed}); e != nil {
		t.Fatal(e)
	}
	// Saved edits remain pending until activation; a failed worker keeps the active
	// configuration and exposes a stable result without replaying the saved request.
	if e = j.Finish(op.ID, nil, fmt.Errorf("activation failed; previous service restored")); e != nil {
		t.Fatal(e)
	}
	state, e = b.State(ctx)
	if e != nil || !state.Pending || state.Active.Config.Routes[0].Target != "127.0.0.1:3000" {
		t.Fatal(state, e)
	}
	raw, _ := json.Marshal(persisted)
	if strings.Contains(string(raw), "3001") {
		t.Fatal("private request exposed")
	}
	saved, e := b.Owner(ctx, OwnerRequest{Action: "config-read"})
	if e != nil || !strings.Contains(string(saved.Data), "Keep this note") {
		t.Fatal("lost comments", e)
	}
}
func TestBrokerRejectsStaleSaveBeforeDispatch(t *testing.T) {
	b := backendFixture(t)
	state, e := b.State(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	if _, e = b.Submit(context.Background(), control.Request{Action: "save", Revision: strings.Repeat("0", 64), Config: &state.Saved.Config}); e == nil {
		t.Fatal("accepted stale save")
	}
	j, e := control.SnapshotOperations(b.Directory)
	if e != nil || j.Active() != nil {
		t.Fatal("stale save was enqueued", e)
	}
}
func TestUnixBrokerEndToEndCredentials(t *testing.T) {
	b := backendFixture(t)
	auth, e := control.NewAuth(filepath.Join(t.TempDir(), "sessions.json"))
	if e != nil {
		t.Fatal(e)
	}
	dir, err := os.MkdirTemp("", "rmgr-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	socket := filepath.Join(dir, "broker.sock")
	listener, e := net.Listen("unix", socket)
	if e != nil {
		t.Fatal(e)
	}
	server := control.UnixServer(control.Handler(b, auth, "owner-secret", "proxy-secret"), "owner-secret", os.Geteuid())
	go func() { _ = server.Serve(listener) }()
	defer server.Close()
	client := control.NewClient(socket, "owner-secret")
	defer client.HTTP.CloseIdleConnections()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var launch struct {
		Token string `json:"token"`
	}
	if e = client.Call(ctx, "POST", control.Prefix+"launch", nil, &launch); e != nil || launch.Token == "" {
		t.Fatal(e)
	}
	req, e := http.NewRequestWithContext(ctx, "POST", "http://control"+control.Prefix+"session", strings.NewReader(`{"token":"`+launch.Token+`"}`))
	if e != nil {
		t.Fatal(e)
	}
	req.Host = "raymond.test"
	req.Header.Set("Authorization", "Bearer proxy-secret")
	req.Header.Set("Origin", control.Origin)
	req.Header.Set("Content-Type", "application/json")
	response, e := client.HTTP.Do(req)
	if e != nil {
		t.Fatal(e)
	}
	defer response.Body.Close()
	if response.StatusCode != 200 || len(response.Cookies()) != 1 {
		t.Fatal("session exchange failed", response.StatusCode)
	}
	var session map[string]string
	if e = json.NewDecoder(response.Body).Decode(&session); e != nil {
		t.Fatal(e)
	}
	request, _ := http.NewRequestWithContext(ctx, "POST", "http://control"+control.Prefix+"operations", strings.NewReader(`{"action":"restart"}`))
	request.Host = "raymond.test"
	request.Header.Set("Authorization", "Bearer proxy-secret")
	request.Header.Set("Origin", control.Origin)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Raymond-CSRF", session["csrf"])
	request.AddCookie(response.Cookies()[0])
	result, e := client.HTTP.Do(request)
	if e != nil {
		t.Fatal(e)
	}
	defer result.Body.Close()
	if result.StatusCode != 202 {
		t.Fatal("authorized browser operation rejected", result.StatusCode)
	}
}
