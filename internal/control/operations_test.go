package control

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/cbspire/raymond/internal/config"
)

func TestOperationsPersistAndSerialize(t *testing.T) {
	dir := t.TempDir()
	j, e := OpenOperations(dir)
	if e != nil {
		t.Fatal(e)
	}
	op, e := j.Enqueue(Request{Action: "restart"})
	if e != nil {
		t.Fatal(e)
	}
	if op.Status != "queued" {
		t.Fatal(op)
	}
	if _, e = j.Enqueue(Request{Action: "stop"}); e == nil {
		t.Fatal("accepted conflicting operation")
	}
	if _, e = OpenOperations(dir); e == nil {
		t.Fatal("accepted second journal owner")
	}
	if e = j.Close(); e != nil {
		t.Fatal(e)
	}
	j, e = OpenOperations(dir)
	if e != nil {
		t.Fatal(e)
	}
	defer j.Close()
	got, e := j.Get(op.ID)
	if e != nil || got.Status != "queued" || got.Request.Action != "restart" {
		t.Fatalf("%+v %v", got, e)
	}
	if e = j.Start(op.ID); e != nil {
		t.Fatal(e)
	}
	if e = j.Progress(op.ID, "restarting"); e != nil {
		t.Fatal(e)
	}
	if e = j.Finish(op.ID, nil, nil); e != nil {
		t.Fatal(e)
	}
	got, e = j.Get(op.ID)
	if e != nil || got.Status != "succeeded" || got.Phase != "" {
		t.Fatalf("%+v %v", got, e)
	}
	if _, e = j.Enqueue(Request{Action: "stop"}); e != nil {
		t.Fatal(e)
	}
}

func TestConcurrentOperationsHaveOneWinner(t *testing.T) {
	j, e := OpenOperations(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer j.Close()
	var wg sync.WaitGroup
	results := make(chan error, 16)
	for range 16 {
		wg.Go(func() { _, e := j.Enqueue(Request{Action: "restart"}); results <- e })
	}
	wg.Wait()
	close(results)
	winners := 0
	for e := range results {
		if e == nil {
			winners++
			continue
		}
		var ce *Error
		if !errors.As(e, &ce) || ce.Status != 409 {
			t.Fatal(e)
		}
	}
	if winners != 1 {
		t.Fatalf("%d operations accepted", winners)
	}
}

func TestOperationRequestSnapshots(t *testing.T) {
	j, e := OpenOperations(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer j.Close()
	c, e := config.Decode(strings.NewReader("version: 1\ndns:\n  zones: [test]\nroutes:\n  - domain: app.test\n    target: 127.0.0.1:9001\n"))
	if e != nil {
		t.Fatal(e)
	}
	op, e := j.Enqueue(Request{Action: "save", Revision: strings.Repeat("a", 64), Config: &c})
	if e != nil {
		t.Fatal(e)
	}
	c.Routes[0].Target = "127.0.0.1:9002"
	op.Request.Config.DNS.Zones[0] = "other"
	got, e := j.Get(op.ID)
	if e != nil {
		t.Fatal(e)
	}
	if got.Request.Config.Routes[0].Target != "127.0.0.1:9001" || got.Request.Config.DNS.Zones[0] != "test" {
		t.Fatal("caller mutated stored request")
	}
	b, e := json.Marshal(got)
	if e != nil {
		t.Fatal(e)
	}
	if strings.Contains(string(b), "revision") || strings.Contains(string(b), "9001") {
		t.Fatal("API exposes stored request")
	}
}

func TestOperationTransitionsAndSnapshots(t *testing.T) {
	j, e := OpenOperations(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer j.Close()
	op, e := j.Enqueue(Request{Action: "restart"})
	if e != nil {
		t.Fatal(e)
	}
	if e = j.Finish(op.ID, nil, nil); e == nil {
		t.Fatal("finished queued work")
	}
	if e = j.Progress(op.ID, "starting"); e == nil {
		t.Fatal("progressed queued work")
	}
	if e = j.Start(op.ID); e != nil {
		t.Fatal(e)
	}
	if e = j.Start(op.ID); e == nil {
		t.Fatal("started twice")
	}
	active := j.Active()
	active.Status = "succeeded"
	if j.Active().Status != "running" {
		t.Fatal("caller changed journal")
	}
	if e = j.Finish(op.ID, json.RawMessage(`{"ok":true}`), nil); e != nil {
		t.Fatal(e)
	}
	got, e := j.Get(op.ID)
	if e != nil {
		t.Fatal(e)
	}
	got.Result[0] = 'x'
	got, e = j.Get(op.ID)
	if e != nil || !json.Valid(got.Result) {
		t.Fatal("caller changed stored result")
	}
	if j.Active() != nil {
		t.Fatal("terminal work remains active")
	}
	if e = j.Finish(op.ID, nil, nil); e == nil {
		t.Fatal("finished twice")
	}
	if e = j.Progress(op.ID, "late"); e == nil {
		t.Fatal("changed terminal work")
	}
	if _, e = j.Get("../owner.json"); e == nil {
		t.Fatal("accepted unknown operation")
	}
}

func TestOperationPersistenceFailureRetainsLock(t *testing.T) {
	dir := t.TempDir()
	j, e := OpenOperations(dir)
	if e != nil {
		t.Fatal(e)
	}
	defer j.Close()
	op, e := j.Enqueue(Request{Action: "restart"})
	if e != nil {
		t.Fatal(e)
	}
	if e = os.Remove(j.path); e != nil {
		t.Fatal(e)
	}
	if e = os.Mkdir(j.path, 0700); e != nil {
		t.Fatal(e)
	}
	if e = j.Start(op.ID); e == nil {
		t.Fatal("ignored persistence failure")
	}
	if _, e = OpenOperations(dir); e == nil {
		t.Fatal("released live worker lock after write failure")
	}
	if e = os.Remove(j.path); e != nil {
		t.Fatal(e)
	}
	if e = j.Start(op.ID); e == nil {
		t.Fatal("continued after uncertain disk state")
	}
}

func TestOperationHistoryIsBounded(t *testing.T) {
	dir := t.TempDir()
	j, e := OpenOperations(dir)
	if e != nil {
		t.Fatal(e)
	}
	largeResult := json.RawMessage(`"` + strings.Repeat("x", (64<<10)-2) + `"`)
	var first, last string
	for i := 0; i < operationHistory+35; i++ {
		op, e := j.Enqueue(Request{Action: "diagnostics"})
		if e != nil {
			t.Fatal(e)
		}
		if i == 0 {
			first = op.ID
		}
		last = op.ID
		if e = j.Start(op.ID); e != nil {
			t.Fatal(e)
		}
		result := json.RawMessage(`true`)
		if i >= operationHistory {
			result = largeResult
		}
		if e = j.Finish(op.ID, result, nil); e != nil {
			t.Fatal(e)
		}
	}
	if _, e = j.Get(first); e == nil {
		t.Fatal("history never evicted old work")
	}
	if e = j.Close(); e != nil {
		t.Fatal(e)
	}
	j, e = OpenOperations(dir)
	if e != nil {
		t.Fatal(e)
	}
	defer j.Close()
	if _, e = j.Get(last); e != nil {
		t.Fatal(e)
	}
	st, e := os.Stat(j.path)
	if e != nil || st.Size() > operationJournalLimit {
		t.Fatalf("unbounded history: %v %v", st, e)
	}
}

func TestOperationsInterruptedWorkRequiresRecovery(t *testing.T) {
	dir := t.TempDir()
	j, e := OpenOperations(dir)
	if e != nil {
		t.Fatal(e)
	}
	op, e := j.Enqueue(Request{Action: "restart"})
	if e != nil {
		t.Fatal(e)
	}
	if e = j.Start(op.ID); e != nil {
		t.Fatal(e)
	}
	if e = j.Progress(op.ID, "activating"); e != nil {
		t.Fatal(e)
	}
	if e = j.Close(); e != nil {
		t.Fatal(e)
	}
	j, e = OpenOperations(dir)
	if e != nil {
		t.Fatal(e)
	}
	defer j.Close()
	got, e := j.Get(op.ID)
	if e != nil || got.Status != "running" || got.Phase != "activating" {
		t.Fatalf("lost recovery evidence: %+v %v", got, e)
	}
	if e = j.Start(op.ID); e == nil {
		t.Fatal("replayed interrupted action")
	}
	if _, e = j.Enqueue(Request{Action: "stop"}); e == nil {
		t.Fatal("interrupted operation did not block writes")
	}
	if e = j.Finish(op.ID, nil, errors.New("previous service restored")); e != nil {
		t.Fatal(e)
	}
	got, e = j.Get(op.ID)
	if e != nil || got.Status != "failed" || got.Error != "previous service restored" {
		t.Fatalf("%+v %v", got, e)
	}
}

func TestOperationsRejectInvalidRequests(t *testing.T) {
	j, e := OpenOperations(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer j.Close()
	for _, req := range []Request{
		{Action: "exec"}, {Action: ""}, {Action: "restart", Revision: "unexpected"},
		{Action: "stop", Version: "0.1.2"}, {Action: "save"},
		{Action: "update", Version: "https://attacker.invalid/a"},
	} {
		if _, e = j.Enqueue(req); e == nil {
			t.Fatalf("accepted %+v", req)
		}
	}
}

func TestOperationsValidateDiskState(t *testing.T) {
	for name, data := range map[string]string{
		"broken": "{", "unknown version": `{"version":2,"operations":[]}`,
		"unknown fields": `{"version":1,"operations":[],"command":"exec"}`,
		"invalid record": `{"version":1,"operations":[{"id":"../x","action":"stop","status":"queued"}]}`,
		"trailing value": `{"version":1,"operations":[]} {}`,
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			if e := os.WriteFile(filepath.Join(dir, "operations.json"), []byte(data), 0600); e != nil {
				t.Fatal(e)
			}
			if j, e := OpenOperations(dir); e == nil {
				j.Close()
				t.Fatal("accepted corrupt journal")
			}
		})
	}
}

func TestOperationsRejectSymlinks(t *testing.T) {
	for _, file := range []string{"operations.lock", "operations.json"} {
		t.Run(file, func(t *testing.T) {
			dir := t.TempDir()
			target := filepath.Join(t.TempDir(), "target")
			if e := os.WriteFile(target, []byte(`{"version":1,"operations":[]}`), 0600); e != nil {
				t.Fatal(e)
			}
			if e := os.Symlink(target, filepath.Join(dir, file)); e != nil {
				t.Fatal(e)
			}
			if j, e := OpenOperations(dir); e == nil {
				j.Close()
				t.Fatal("followed symlink")
			}
		})
	}
}

func TestOperationsRejectUnsafeFiles(t *testing.T) {
	for _, file := range []string{"operations.lock", "operations.json"} {
		for _, kind := range []string{"public", "hardlink", "directory"} {
			t.Run(file+"/"+kind, func(t *testing.T) {
				dir := t.TempDir()
				p := filepath.Join(dir, file)
				if kind == "directory" {
					if e := os.Mkdir(p, 0700); e != nil {
						t.Fatal(e)
					}
				} else {
					if e := os.WriteFile(p, []byte(`{"version":1,"operations":[]}`), 0600); e != nil {
						t.Fatal(e)
					}
					if kind == "public" {
						if e := os.Chmod(p, 0644); e != nil {
							t.Fatal(e)
						}
					} else if e := os.Link(p, filepath.Join(dir, "alias")); e != nil {
						t.Fatal(e)
					}
				}
				if j, e := OpenOperations(dir); e == nil {
					j.Close()
					t.Fatal("accepted unsafe journal file")
				}
			})
		}
	}
}

func TestInheritedWorkerLockAndSnapshot(t *testing.T) {
	dir := t.TempDir()
	broker, e := OpenOperations(dir)
	if e != nil {
		t.Fatal(e)
	}
	op, e := broker.Enqueue(Request{Action: "restart"})
	if e != nil {
		t.Fatal(e)
	}
	fd, e := broker.WorkerLock()
	if e != nil {
		t.Fatal(e)
	}
	defer fd.Close()
	if e = broker.Close(); e != nil {
		t.Fatal(e)
	}
	if another, e := OpenOperations(dir); e == nil {
		another.Close()
		t.Fatal("broker close released inherited lock")
	}
	worker, e := AdoptOperations(dir, fd)
	if e != nil {
		t.Fatal(e)
	}
	if e = worker.Start(op.ID); e != nil {
		t.Fatal(e)
	}
	snapshot, e := SnapshotOperations(dir)
	if e != nil || snapshot.Active() == nil || snapshot.Active().Status != "running" {
		t.Fatal("API cannot read live worker", e)
	}
	if e = worker.Finish(op.ID, nil, nil); e != nil {
		t.Fatal(e)
	}
	if e = worker.Close(); e != nil {
		t.Fatal(e)
	}
	next, e := OpenOperations(dir)
	if e != nil {
		t.Fatal(e)
	}
	defer next.Close()
	if next.Active() != nil {
		t.Fatal("terminal work remains active")
	}
}
