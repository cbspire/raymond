package control

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/cbspire/raymond/internal/release"
	"golang.org/x/sys/unix"
)

const operationHistory = 128
const operationJournalLimit = 2 << 20

// A separate record keeps the request durable without exposing it in API replies.
type operationRecord struct {
	Operation Operation `json:"operation"`
	Request   Request   `json:"request"`
}
type operationData struct {
	Version    int               `json:"version"`
	Operations []operationRecord `json:"operations"`
}

// Operations serializes management work and durably records its transitions.
// Its caller must supply a private, trusted directory and retain ownership for
// the entire worker lifetime. A replacement manager cannot take over a live
// worker's journal. Running work found after acquiring the lock requires explicit
// reconciliation; it must never be automatically replayed.
type Operations struct {
	mu      sync.Mutex
	path    string
	lock    *os.File
	data    operationData
	failure error
}

func privateFile(path string, flags int) (*os.File, error) {
	fd, e := unix.Open(path, flags|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0600)
	if e != nil {
		return nil, e
	}
	f := os.NewFile(uintptr(fd), path)
	var st unix.Stat_t
	if e = unix.Fstat(fd, &st); e == nil {
		if st.Mode&unix.S_IFMT != unix.S_IFREG || int(st.Uid) != os.Geteuid() || st.Mode&0077 != 0 || st.Nlink != 1 {
			e = fmt.Errorf("management journal files must be private regular files owned by the manager")
		}
	}
	if e != nil {
		f.Close()
		return nil, e
	}
	return f, nil
}

func OpenOperations(dir string) (*Operations, error) {
	if !filepath.IsAbs(dir) || filepath.Clean(dir) != dir {
		return nil, fmt.Errorf("invalid operation directory")
	}
	st, e := os.Lstat(dir)
	if e != nil {
		return nil, e
	}
	if !st.IsDir() || st.Mode().Perm()&0077 != 0 {
		return nil, fmt.Errorf("operation directory must be private")
	}
	dfd, e := unix.Open(dir, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if e != nil {
		return nil, e
	}
	var ds unix.Stat_t
	e = unix.Fstat(dfd, &ds)
	unix.Close(dfd)
	if e != nil {
		return nil, e
	}
	if int(ds.Uid) != os.Geteuid() {
		return nil, fmt.Errorf("operation directory must belong to the manager")
	}
	f, e := privateFile(filepath.Join(dir, "operations.lock"), unix.O_CREAT|unix.O_RDWR)
	if e != nil {
		return nil, e
	}
	if e = unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); e != nil {
		f.Close()
		return nil, Conflict("another management worker owns the operation journal")
	}
	j := &Operations{path: filepath.Join(dir, "operations.json"), lock: f, data: operationData{Version: 1, Operations: []operationRecord{}}}
	if e = j.load(); e != nil {
		j.Close()
		return nil, e
	}
	return j, nil
}

func (j *Operations) load() error {
	f, e := privateFile(j.path, unix.O_RDONLY)
	if os.IsNotExist(e) {
		return nil
	}
	if e != nil {
		return e
	}
	defer f.Close()
	b, e := io.ReadAll(io.LimitReader(f, operationJournalLimit+1))
	if e != nil {
		return e
	}
	if len(b) > operationJournalLimit {
		return fmt.Errorf("operation journal too large")
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if e = d.Decode(&j.data); e != nil {
		return e
	}
	var extra any
	if e = d.Decode(&extra); e != io.EOF {
		return fmt.Errorf("invalid operation journal trailing data")
	}
	if j.data.Version != 1 || len(j.data.Operations) > operationHistory {
		return fmt.Errorf("unsupported operation journal")
	}
	seen, active := map[string]bool{}, 0
	for i, r := range j.data.Operations {
		o := r.Operation
		if !validOperationID(o.ID) || seen[o.ID] || o.Action != r.Request.Action || o.Created.IsZero() || o.Updated.Before(o.Created) {
			return fmt.Errorf("invalid operation journal record")
		}
		if e = validateRequest(r.Request); e != nil {
			return e
		}
		seen[o.ID] = true
		switch o.Status {
		case "queued", "running":
			active++
			if i != len(j.data.Operations)-1 || len(o.Result) != 0 {
				return fmt.Errorf("invalid active operation record")
			}
		case "succeeded", "failed":
		default:
			return fmt.Errorf("invalid operation status")
		}
		if o.Status != "failed" && o.Error != "" {
			return fmt.Errorf("invalid operation error")
		}
		if len(o.Phase) > 256 || len(o.Error) > 4096 || len(o.Result) > 64<<10 || (o.Status != "running" && o.Phase != "") {
			return fmt.Errorf("invalid operation progress or result")
		}
	}
	if active > 1 {
		return fmt.Errorf("conflicting active operations in journal")
	}
	return nil
}

func validOperationID(s string) bool {
	if len(s) != 26 {
		return false
	}
	for _, c := range s {
		if !strings.ContainsRune("abcdefghijklmnopqrstuvwxyz234567", c) {
			return false
		}
	}
	return true
}

func validateRequest(r Request) error {
	switch r.Action {
	case "save":
		if r.Config == nil || len(r.Revision) != 64 || r.Version != "" {
			return fmt.Errorf("save requires a configuration and revision")
		}
		for _, c := range r.Revision {
			if !strings.ContainsRune("0123456789abcdef", c) {
				return fmt.Errorf("invalid configuration revision")
			}
		}
		return r.Config.Validate()
	case "update":
		if r.Config != nil || r.Revision != "" || (r.Version != "" && !release.ValidVersion(r.Version)) {
			return fmt.Errorf("invalid update request")
		}
	case "start", "stop", "restart", "retry", "discard", "diagnostics", "check-update":
		if r.Config != nil || r.Revision != "" || r.Version != "" {
			return fmt.Errorf("unexpected operation arguments")
		}
	default:
		return fmt.Errorf("unsupported management action")
	}
	return nil
}

func (j *Operations) Close() error {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.lock == nil {
		return nil
	}
	e := j.lock.Close()
	j.lock = nil
	return e
}

// publish commits before updating memory. A durability failure poisons further
// writes while retaining the worker lock until Close, so a second manager cannot
// begin work while an existing worker may still be unwinding.
func (j *Operations) publish(next operationData) error {
	if j.lock == nil {
		return fmt.Errorf("operation journal is closed")
	}
	if j.failure != nil {
		return fmt.Errorf("operation journal requires recovery: %w", j.failure)
	}
	b, e := json.Marshal(next)
	if e != nil {
		return e
	}
	for len(b) > operationJournalLimit && len(next.Operations) > 1 {
		next.Operations = next.Operations[1:]
		b, e = json.Marshal(next)
		if e != nil {
			return e
		}
	}
	if len(b) > operationJournalLimit {
		return fmt.Errorf("operation journal too large")
	}
	if e = AtomicWrite(j.path, b, 0600); e != nil {
		j.failure = e
		return e
	}
	j.data = next
	return nil
}

func cloneRecord(r operationRecord) operationRecord {
	r.Operation.Result = bytes.Clone(r.Operation.Result)
	if r.Request.Config != nil {
		c := *r.Request.Config
		c.Routes = append(c.Routes[:0:0], c.Routes...)
		c.DNS.Zones = append(c.DNS.Zones[:0:0], c.DNS.Zones...)
		r.Request.Config = &c
	}
	r.Operation.Request = r.Request
	return r
}

func (j *Operations) Enqueue(r Request) (Operation, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	// Validate an owned copy; config validation normalizes its slices.
	record := cloneRecord(operationRecord{Request: r})
	if e := validateRequest(record.Request); e != nil {
		return Operation{}, e
	}
	for _, old := range j.data.Operations {
		if old.Operation.Status == "queued" || old.Operation.Status == "running" {
			return Operation{}, Conflict("a management operation is already active")
		}
	}
	now := time.Now().UTC()
	record.Operation = Operation{ID: strings.ToLower(rand.Text()), Action: r.Action, Status: "queued", Created: now, Updated: now}
	next := operationData{Version: 1, Operations: append([]operationRecord(nil), j.data.Operations...)}
	if len(next.Operations) == operationHistory {
		next.Operations = next.Operations[1:]
	}
	next.Operations = append(next.Operations, record)
	if e := j.publish(next); e != nil {
		return Operation{}, e
	}
	return cloneRecord(record).Operation, nil
}

func (j *Operations) Get(id string) (Operation, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	for _, r := range j.data.Operations {
		if r.Operation.ID == id {
			return cloneRecord(r).Operation, nil
		}
	}
	return Operation{}, &Error{404, "operation not found"}
}

func (j *Operations) Active() *Operation {
	j.mu.Lock()
	defer j.mu.Unlock()
	for _, r := range j.data.Operations {
		if r.Operation.Status == "queued" || r.Operation.Status == "running" {
			o := cloneRecord(r).Operation
			return &o
		}
	}
	return nil
}

func (j *Operations) change(id string, f func(*Operation) error) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	next := operationData{Version: 1, Operations: append([]operationRecord(nil), j.data.Operations...)}
	for i := range next.Operations {
		o := &next.Operations[i].Operation
		if o.ID != id {
			continue
		}
		if e := f(o); e != nil {
			return e
		}
		o.Updated = time.Now().UTC()
		return j.publish(next)
	}
	return &Error{404, "operation not found"}
}

func (j *Operations) Start(id string) error {
	return j.change(id, func(o *Operation) error {
		if o.Status != "queued" {
			return Conflict("operation is not queued; reconcile interrupted work before continuing")
		}
		o.Status = "running"
		return nil
	})
}

func (j *Operations) Progress(id, phase string) error {
	if len(phase) > 256 {
		return fmt.Errorf("operation phase too long")
	}
	return j.change(id, func(o *Operation) error {
		if o.Status != "running" {
			return Conflict("operation is not running")
		}
		o.Phase = phase
		return nil
	})
}

func (j *Operations) Finish(id string, result json.RawMessage, failure error) error {
	if len(result) > 64<<10 || (len(result) > 0 && !json.Valid(result)) {
		return fmt.Errorf("invalid operation result")
	}
	message := ""
	if failure != nil {
		message = failure.Error()
		if len(message) > 4096 {
			message = message[:4096]
		}
	}
	return j.change(id, func(o *Operation) error {
		if o.Status != "running" {
			return Conflict("operation is not running")
		}
		o.Status, o.Phase, o.Error, o.Result = "succeeded", "", message, bytes.Clone(result)
		if failure != nil {
			o.Status = "failed"
		}
		return nil
	})
}

// SnapshotOperations reads an atomically published snapshot without taking the
// worker lock. API readers must remain available while a detached worker runs.
func SnapshotOperations(dir string) (*Operations, error) {
	j := &Operations{path: filepath.Join(dir, "operations.json"), data: operationData{Version: 1}}
	if e := j.load(); e != nil {
		return nil, e
	}
	return j, nil
}

// WorkerLock duplicates the open file description, retaining its flock across
// exec through ExtraFiles. Closing the broker's descriptors cannot unlock a live worker.
func (j *Operations) WorkerLock() (*os.File, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.lock == nil || j.failure != nil {
		return nil, fmt.Errorf("journal unavailable")
	}
	fd, e := unix.Dup(int(j.lock.Fd()))
	if e != nil {
		return nil, e
	}
	unix.CloseOnExec(fd)
	return os.NewFile(uintptr(fd), "operations.lock"), nil
}

func AdoptOperations(dir string, f *os.File) (*Operations, error) {
	expected, e := privateFile(filepath.Join(dir, "operations.lock"), unix.O_RDWR)
	if e != nil {
		return nil, e
	}
	defer expected.Close()
	a, e := f.Stat()
	if e != nil {
		return nil, e
	}
	b, e := expected.Stat()
	if e != nil {
		return nil, e
	}
	if !os.SameFile(a, b) {
		return nil, fmt.Errorf("invalid inherited operation lock")
	}
	// ExtraFiles deliberately clears CLOEXEC for the worker handoff. Restore it
	// before spawning any dropped-UID helper or native tool.
	unix.CloseOnExec(int(f.Fd()))
	if e = unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); e != nil {
		return nil, e
	}
	j := &Operations{path: filepath.Join(dir, "operations.json"), lock: f, data: operationData{Version: 1}}
	if e = j.load(); e != nil {
		return nil, e
	}
	return j, nil
}
