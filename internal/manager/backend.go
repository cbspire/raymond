package manager

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/cbspire/raymond/internal/config"
	"github.com/cbspire/raymond/internal/control"
	"github.com/cbspire/raymond/internal/install"
	"github.com/cbspire/raymond/internal/release"
)

type releaseSource interface {
	Latest(context.Context) (string, error)
	Download(context.Context, string, string, string, string) (string, error)
}

type Backend struct {
	releases          releaseSource
	RequireEnrollment bool
	Metadata          control.Metadata
	Directory         string
	Version           string
	Owner             func(context.Context, OwnerRequest) (OwnerResult, error)
	Active            func() (control.ConfigView, error)
	Running           func() bool
	Spawn             func(*control.Operations) error
	streams           chan struct{}
}

func NewBackend(m control.Metadata, version string) *Backend {
	return &Backend{releases: release.NewClient(), RequireEnrollment: true, Metadata: m, Directory: control.RootDir, Version: version,
		Owner: func(ctx context.Context, r OwnerRequest) (OwnerResult, error) {
			return OwnerCall(ctx, install.ManagedBinary(), m, r)
		},
		Active: readActive, Running: install.ServiceRunning, Spawn: spawnWorker, streams: make(chan struct{}, 4)}
}
func configView(b []byte) (control.ConfigView, error) {
	c, e := config.Decode(bytes.NewReader(b))
	if e != nil {
		return control.ConfigView{}, e
	}
	if c.Routes == nil {
		c.Routes = []config.Route{}
	}
	return control.ConfigView{Config: c, Revision: config.Revision(b)}, nil
}
func canonical(c config.Config) string { b, _ := json.Marshal(c); return config.Revision(b) }

type activation struct {
	Revision string `json:"revision"`
	Hash     string `json:"hash"`
}

func readActive() (control.ConfigView, error) {
	if transaction, err := install.LoadTransaction(); err == nil {
		if transaction.Phase != "committed" && transaction.Phase != "rolled-back" && !transaction.NewInstall {
			data, e := install.PreviousConfig(transaction)
			if e != nil {
				return control.ConfigView{}, e
			}
			view, e := configView(data)
			view.Revision = transaction.Revision
			return view, e
		}
	} else if !os.IsNotExist(err) {
		return control.ConfigView{}, err
	}
	if _, e := install.Installed(); e != nil {
		return control.ConfigView{}, e
	}
	b, e := install.ReadManagedConfig()
	if e != nil {
		return control.ConfigView{}, e
	}
	v, e := configView(b)
	if e != nil {
		return v, e
	}
	path := filepath.Join(control.RootDir, "active.json")
	if e = install.Trusted(path); e != nil {
		return v, e
	}
	if data, err := os.ReadFile(path); err == nil {
		var stamp activation
		if err = json.Unmarshal(data, &stamp); err != nil {
			return v, err
		}
		if stamp.Hash == canonical(v.Config) {
			v.Revision = stamp.Revision
		}
	} else if !os.IsNotExist(err) {
		return v, err
	}
	return v, nil
}
func writeActive(revision string) error {
	b, e := install.ReadManagedConfig()
	if e != nil {
		return e
	}
	v, e := configView(b)
	if e != nil {
		return e
	}
	return control.AtomicJSON(filepath.Join(control.RootDir, "active.json"), activation{Revision: revision, Hash: canonical(v.Config)})
}
func (b *Backend) State(ctx context.Context) (control.State, error) {
	s := control.State{Enrolled: install.ManagementReady(), Version: b.Version, Platform: runtime.GOOS + "/" + runtime.GOARCH, ConfigPath: b.Metadata.ConfigPath}
	owned, e := b.Owner(ctx, OwnerRequest{Action: "config-read", Path: b.Metadata.ConfigPath})
	if e == nil {
		s.Saved, e = configView(owned.Data)
	}
	if e != nil {
		s.Saved.Error = e.Error()
	}
	s.Active, e = b.Active()
	if e != nil {
		s.Active.Error = e.Error()
	}
	s.Running = b.Running()
	s.Pending = s.Saved.Error != "" || s.Active.Error != "" || canonical(s.Saved.Config) != canonical(s.Active.Config)
	j, e := control.SnapshotOperations(b.Directory)
	if e != nil {
		return s, e
	}
	s.Operation = j.Active()
	return s, nil
}
func (b *Backend) Submit(ctx context.Context, r control.Request) (control.Operation, error) {
	if b.RequireEnrollment && !install.ManagementReady() {
		return control.Operation{}, &control.Error{Status: 503, Message: "enrollment incomplete; run raymond setup"}
	}
	j, e := openSubmissionJournal(ctx, b.Directory)
	if e != nil {
		return control.Operation{}, e
	}
	defer j.Close()
	if r.Action == "save" {
		saved, err := b.Owner(ctx, OwnerRequest{Action: "config-read", Path: b.Metadata.ConfigPath})
		if err != nil {
			return control.Operation{}, err
		}
		if saved.Revision != r.Revision {
			return control.Operation{}, control.Conflict("configuration changed externally; reload before saving")
		}
	}
	if j.Active() == nil && b.RequireEnrollment {
		if e = cleanupCompleted(j); e != nil {
			return control.Operation{}, e
		}
	}
	op, e := j.Enqueue(r)
	if e != nil {
		return op, e
	}
	if e = b.Spawn(j); e != nil {
		if err := j.Start(op.ID); err == nil {
			_ = j.Finish(op.ID, nil, e)
		}
		return op, fmt.Errorf("start management worker: %w", e)
	}
	return op, nil
}
func (b *Backend) GetOperation(id string) (control.Operation, error) {
	j, e := control.SnapshotOperations(b.Directory)
	if e != nil {
		return control.Operation{}, e
	}
	return j.Get(id)
}
func (b *Backend) recoverWorker() error {
	j, e := control.OpenOperations(b.Directory)
	if e != nil {
		var conflict *control.Error
		if asConflict(e, &conflict) {
			return nil
		}
		return e
	}
	defer j.Close()
	if j.Active() == nil {
		return cleanupCompleted(j)
	}
	return b.Spawn(j)
}
func asConflict(e error, out **control.Error) bool {
	ce, ok := e.(*control.Error)
	if ok && ce.Status == http.StatusConflict {
		*out = ce
		return true
	}
	return false
}

// A terminal result becomes durable before its worker removes rollback artifacts
// and releases the lock. Wait briefly for that cleanup (or an idle recovery tick),
// while keeping genuinely queued/running work an immediate conflict.
func openSubmissionJournal(ctx context.Context, directory string) (*control.Operations, error) {
	deadline := time.NewTimer(2 * time.Second)
	defer deadline.Stop()
	for {
		if e := ctx.Err(); e != nil {
			return nil, e
		}
		journal, e := control.OpenOperations(directory)
		if e == nil {
			return journal, nil
		}
		var conflict *control.Error
		if !asConflict(e, &conflict) {
			return nil, e
		}
		snapshot, err := control.SnapshotOperations(directory)
		if err != nil {
			return nil, err
		}
		if snapshot.Active() != nil {
			return nil, e
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-deadline.C:
			return nil, e
		case <-time.After(10 * time.Millisecond):
		}
	}
}
