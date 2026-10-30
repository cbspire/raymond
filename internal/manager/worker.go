package manager

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/cbspire/raymond/internal/control"
	diagnostic "github.com/cbspire/raymond/internal/diagnostics"
	"github.com/cbspire/raymond/internal/install"
	"github.com/cbspire/raymond/internal/release"
	mdns "github.com/miekg/dns"
)

func Worker(version string, lock *os.File) error {
	if os.Geteuid() != 0 {
		return fmt.Errorf("operation worker requires root")
	}
	m, e := install.ManagementMetadata()
	if e != nil {
		return e
	}
	j, e := control.AdoptOperations(control.RootDir, lock)
	if e != nil {
		return e
	}
	defer j.Close()
	op := j.Active()
	if op == nil {
		return fmt.Errorf("no pending operation")
	}
	unlock, e := install.ManagedLock()
	if e != nil {
		return e
	}
	defer unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	b := NewBackend(m, version)
	return executeWorker(ctx, b, j, *op)
}

func executeWorker(ctx context.Context, b *Backend, j *control.Operations, current control.Operation) error {
	op := &current
	var e error
	if op.Status == "running" {
		return recoverOperation(ctx, b, j, *op)
	}
	if e = j.Start(op.ID); e != nil {
		return e
	}
	result, e := runOperation(ctx, b, j, *op)
	if e != nil {
		if t, err := install.LoadTransaction(); err == nil {
			if t.Operation != op.ID {
				return fmt.Errorf("transaction operation mismatch")
			}
			if rollback := restoreTransaction(ctx, b, t); rollback != nil {
				return errors.Join(e, fmt.Errorf("rollback incomplete; evidence retained: %w", rollback))
			}
			e = fmt.Errorf("operation failed; previous working service restored: %w", e)
		} else if !os.IsNotExist(err) {
			return errors.Join(e, err)
		}
	}
	if err := j.Finish(op.ID, result, e); err != nil {
		return err
	}
	if t, err := install.LoadTransaction(); err == nil && t.NewInstall && t.Phase == "rolled-back" {
		return nil
	}
	if e := install.ClearTransaction(); e != nil {
		return e
	}
	return clearStaging()
}
func recoverOperation(ctx context.Context, b *Backend, j *control.Operations, op control.Operation) error {
	t, e := install.LoadTransaction()
	if os.IsNotExist(e) {
		if e = j.Finish(op.ID, nil, fmt.Errorf("operation interrupted; inspect current status before retrying")); e != nil {
			return e
		}
		if e := install.ClearTransaction(); e != nil {
			return e
		}
		return clearStaging()
	}
	if e != nil {
		return e
	}
	if t.Operation != op.ID {
		return fmt.Errorf("rollback evidence belongs to a different operation")
	}
	if t.Phase == "committed" {
		if t.Enrollment {
			if e = FinishEnrollment(ctx, b.Metadata); e != nil {
				return e
			}
		}
		if e = j.Finish(op.ID, nil, nil); e != nil {
			return e
		}
		if e := install.ClearTransaction(); e != nil {
			return e
		}
		return clearStaging()
	}
	if e = restoreTransaction(ctx, b, t); e != nil {
		return e
	}
	if e = j.Finish(op.ID, nil, fmt.Errorf("operation interrupted; previous working service restored")); e != nil {
		return e
	}
	if t, err := install.LoadTransaction(); err == nil && t.NewInstall && t.Phase == "rolled-back" {
		return nil
	}
	if e := install.ClearTransaction(); e != nil {
		return e
	}
	return clearStaging()
}
func restoreTransaction(ctx context.Context, b *Backend, t *install.Transaction) error {
	if e := install.RollbackManaged(t); e != nil {
		return e
	}
	if t.CLIHash != "" {
		current, e := b.Owner(ctx, OwnerRequest{Action: "cli-read", Path: b.Metadata.CLIPath})
		if e != nil {
			return e
		}
		if current.Revision != t.CLIHash {
			if current.Revision != t.NewCLIHash {
				return fmt.Errorf("owner CLI changed outside update; refusing rollback")
			}
			old, e := install.RollbackCLI(t)
			if e != nil {
				return e
			}
			if _, e = b.Owner(ctx, OwnerRequest{Action: "cli-save", Path: b.Metadata.CLIPath, Revision: current.Revision, Data: old}); e != nil {
				return e
			}
		}
		if e = install.StartManagement(true); e != nil {
			return e
		}
	}
	if t.NewInstall {
		return nil
	}
	return writeActive(t.Revision)
}
func runOperation(ctx context.Context, b *Backend, j *control.Operations, op control.Operation) (json.RawMessage, error) {
	progress := func(phase string) error { return j.Progress(op.ID, phase) }
	r := op.Request
	switch r.Action {
	case "start", "restart", "stop":
		if e := progress("Controlling the local service"); e != nil {
			return nil, e
		}
		if e := install.ManagedControl(r.Action); e != nil {
			return nil, e
		}
		if r.Action != "stop" {
			return nil, health(ctx)
		}
		return nil, nil
	case "discard":
		saved, e := b.Owner(ctx, OwnerRequest{Action: "config-read", Path: b.Metadata.ConfigPath})
		if e != nil {
			return nil, e
		}
		active, e := b.Active()
		if e != nil {
			return nil, e
		}
		_, e = b.Owner(ctx, OwnerRequest{Action: "config-discard", Path: b.Metadata.ConfigPath, Revision: saved.Revision, Config: &active.Config})
		return nil, e
	case "diagnostics":
		return diagnostics(ctx, b)
	case "check-update":
		latest, e := b.releases.Latest(ctx)
		if e != nil {
			return nil, e
		}
		return json.Marshal(map[string]any{"current": b.Version, "latest": latest, "available": release.Newer(latest, b.Version) && !release.Newer("0.1.2", latest)})
	case "save", "retry", "update":
		return replace(ctx, b, j, op)
	default:
		return nil, fmt.Errorf("unsupported operation")
	}
}
func replace(ctx context.Context, b *Backend, j *control.Operations, op control.Operation) (json.RawMessage, error) {
	r := op.Request
	if e := j.Progress(op.ID, "Validating owned configuration"); e != nil {
		return nil, e
	}
	if r.Action == "save" {
		if _, e := b.Owner(ctx, OwnerRequest{Action: "config-save", Path: b.Metadata.ConfigPath, Revision: r.Revision, Config: r.Config}); e != nil {
			return nil, e
		}
	}
	saved, e := b.Owner(ctx, OwnerRequest{Action: "config-read", Path: b.Metadata.ConfigPath})
	if e != nil {
		return nil, e
	}
	view, e := configView(saved.Data)
	if e != nil {
		return nil, e
	}
	active, e := b.Active()
	if e != nil {
		return nil, e
	}
	stage := filepath.Join(control.RootDir, "staging")
	if e = install.Trusted(stage); e != nil {
		return nil, e
	}
	if e = os.MkdirAll(stage, 0700); e != nil {
		return nil, e
	}
	cfg := filepath.Join(stage, "config.yaml")
	if e = control.AtomicWrite(cfg, saved.Data, 0600); e != nil {
		return nil, e
	}
	executable := install.ManagedBinary()
	var cli []byte
	target := ""
	if r.Action == "update" {
		if e = j.Progress(op.ID, "Resolving and verifying the native stable release"); e != nil {
			return nil, e
		}
		client := b.releases
		target = r.Version
		if target == "" {
			target, e = client.Latest(ctx)
			if e != nil {
				return nil, e
			}
		}
		if release.Newer("0.1.2", target) || !release.Newer(target, b.Version) {
			return nil, fmt.Errorf("no newer management-compatible stable release")
		}
		download, e := os.MkdirTemp(stage, "release-")
		if e != nil {
			return nil, e
		}
		defer os.RemoveAll(download)
		executable, e = client.Download(ctx, target, runtime.GOOS, runtime.GOARCH, download)
		if e != nil {
			return nil, e
		}
		output, e := exec.CommandContext(ctx, executable, "version").Output()
		if e != nil || !strings.HasPrefix(string(output), "raymond "+target+" (") {
			return nil, fmt.Errorf("downloaded executable version mismatch")
		}
		previous, e := b.Owner(ctx, OwnerRequest{Action: "cli-read", Path: b.Metadata.CLIPath})
		if e != nil {
			return nil, e
		}
		cli = previous.Data
	}
	t, e := install.PrepareManaged(op.ID, active.Revision, executable, cli)
	if e != nil {
		return nil, e
	}
	if e = j.Progress(op.ID, "Applying protected service resources"); e != nil {
		return nil, e
	}
	if e = install.ApplyManaged(t, cfg, executable); e != nil {
		return nil, e
	}
	if e = health(ctx); e != nil {
		return nil, e
	}
	if r.Action == "update" {
		if e = j.Progress(op.ID, "Updating the owner's CLI and restarting management"); e != nil {
			return nil, e
		}
		next, e := os.ReadFile(executable)
		if e != nil {
			return nil, e
		}
		if _, e = b.Owner(ctx, OwnerRequest{Action: "cli-save", Path: b.Metadata.CLIPath, Revision: t.CLIHash, Data: next}); e != nil {
			return nil, e
		}
		if e = install.StartManagement(true); e != nil {
			return nil, e
		}
		if e = waitVersion(ctx, b.Metadata, target); e != nil {
			return nil, e
		}
	}
	if e = writeActive(view.Revision); e != nil {
		return nil, e
	}
	if e = install.CommitManaged(t); e != nil {
		return nil, e
	}
	return json.Marshal(map[string]string{"revision": view.Revision, "version": func() string {
		if target != "" {
			return target
		}
		return b.Version
	}()})
}
func health(ctx context.Context) error {
	client := &http.Client{Transport: &http.Transport{Proxy: nil}, Timeout: time.Second}
	defer client.CloseIdleConnections()
	deadline := time.Now().Add(15 * time.Second)
	for {
		var last error
		for _, address := range []string{"127.0.0.42:80", "127.0.0.43:53"} {
			c, e := (&net.Dialer{Timeout: time.Second}).DialContext(ctx, "tcp4", address)
			if e != nil {
				last = e
				break
			}
			c.Close()
		}
		if last == nil {
			request, _ := http.NewRequestWithContext(ctx, http.MethodHead, "http://127.0.0.42/", nil)
			request.Host = "raymond.test"
			response, e := client.Do(request)
			if e != nil {
				last = e
			} else {
				response.Body.Close()
			}
		}
		if last == nil {
			q := new(mdns.Msg)
			q.SetQuestion("raymond-health.invalid.", mdns.TypeA)
			response, _, e := (&mdns.Client{Timeout: time.Second}).ExchangeContext(ctx, q, "127.0.0.43:53")
			if e == nil && response != nil {
				return nil
			}
			last = e
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("new service did not pass DNS/HTTP health checks: %v", last)
		}
		time.Sleep(100 * time.Millisecond)
	}
}
func waitVersion(ctx context.Context, m control.Metadata, version string) error {
	client := control.NewClient(control.SocketPath, m.OwnerToken)
	defer client.HTTP.CloseIdleConnections()
	deadline := time.Now().Add(15 * time.Second)
	for {
		probe, cancel := context.WithTimeout(ctx, time.Second)
		var state control.State
		e := client.Call(probe, http.MethodGet, control.Prefix+"state", nil, &state)
		cancel()
		if e == nil && state.Version == version {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("replacement manager did not confirm its version")
		}
		time.Sleep(100 * time.Millisecond)
	}
}
func diagnostics(ctx context.Context, b *Backend) (json.RawMessage, error) {
	state, e := b.State(ctx)
	if e != nil {
		return nil, e
	}
	checks := map[string]any{"config": state.Saved.Error == "", "running": state.Running, "pending": state.Pending}
	resolver := install.PlatformCheck(state.Active.Config.DNS.Zones...)
	checks["resolver"] = resolver == nil
	if resolver != nil {
		checks["resolver_error"] = resolver.Error()
	}
	if state.Running {
		checks["listeners"] = health(ctx) == nil
	} else if state.Saved.Error == "" {
		if listenerError := diagnostic.Check(state.Saved.Config); listenerError != nil {
			checks["listeners"] = false
			checks["listener_error"] = listenerError.Error()
		} else {
			checks["listeners"] = true
		}
	}
	backends := map[string]bool{}
	for i, route := range state.Saved.Config.Routes {
		if i >= 100 {
			break
		}
		probe, cancel := context.WithTimeout(ctx, 250*time.Millisecond)
		c, e := (&net.Dialer{}).DialContext(probe, "tcp4", route.Target)
		cancel()
		backends[route.Target] = e == nil
		if c != nil {
			c.Close()
		}
	}
	checks["backends"] = backends
	return json.Marshal(checks)
}

func CheckHealth(ctx context.Context) error { return health(ctx) }
func MarkActive(revision string) error      { return writeActive(revision) }
func FinishEnrollment(ctx context.Context, m control.Metadata) error {
	credential, _ := json.Marshal(map[string]any{"token": m.OwnerToken, "ready": true})
	if _, e := OwnerCall(ctx, install.ManagedBinary(), m, OwnerRequest{Action: "credential", Data: credential}); e != nil {
		return e
	}
	return install.CompleteManagementEnrollment()
}
