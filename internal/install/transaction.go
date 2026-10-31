package install

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/cbspire/raymond/internal/control"
)

const rollbackDir = control.RootDir + "/rollback"
const rollbackManifest = rollbackDir + "/transaction.json"

// Managed transactions retain a usable protected executable throughout replacement.
// The broker and worker may be killed/replaced independently; rollback evidence
// and operation records live outside the proxy installation journal.
type Transaction struct {
	Enrollment    bool   `json:"enrollment,omitempty"`
	NewInstall    bool   `json:"new_install,omitempty"`
	Version       int    `json:"version"`
	Operation     string `json:"operation"`
	Phase         string `json:"phase"`
	Running       bool   `json:"running"`
	Revision      string `json:"revision"`
	BinaryHash    string `json:"binary_hash"`
	ConfigHash    string `json:"config_hash"`
	NewBinaryHash string `json:"new_binary_hash"`
	CLIHash       string `json:"cli_hash,omitempty"`
	NewCLIHash    string `json:"new_cli_hash,omitempty"`
}

func ManagedLock() (func(), error) {
	if e := root(); e != nil {
		return nil, e
	}
	return lifecycleLock()
}
func goodDigest(s string) bool {
	if len(s) != 64 {
		return false
	}
	for _, c := range s {
		if !strings.ContainsRune("0123456789abcdef", c) {
			return false
		}
	}
	return true
}
func goodOperation(s string) bool {
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
func readProtected(path string, limit int64) ([]byte, error) {
	if e := trusted(path); e != nil {
		return nil, e
	}
	f, e := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if e != nil {
		return nil, e
	}
	defer f.Close()
	st, e := f.Stat()
	if e != nil {
		return nil, e
	}
	if !st.Mode().IsRegular() {
		return nil, fmt.Errorf("expected protected regular file")
	}
	b, e := io.ReadAll(io.LimitReader(f, limit+1))
	if e != nil {
		return nil, e
	}
	if int64(len(b)) > limit {
		return nil, fmt.Errorf("protected file too large")
	}
	return b, nil
}
func (t *Transaction) persist() error { return control.AtomicJSON(rollbackManifest, t) }

// PrepareManaged is called with both the operation and lifecycle locks held.
// It does not change installed resources until the complete manifest is durable.
func PrepareManaged(id, revision, executable string, cli []byte, enrollment ...bool) (*Transaction, error) {
	initial := len(enrollment) > 0 && enrollment[0]
	fresh := false
	bootstrapKnown := false
	if initial {
		if old, err := LoadTransaction(); err == nil && old.NewInstall && old.Phase == "rolled-back" {
			b, e := readProtected(binaryPath(), 256<<20)
			if e != nil && !os.IsNotExist(e) {
				return nil, e
			}
			if e == nil && digest(b) != old.NewBinaryHash {
				return nil, fmt.Errorf("bootstrap binary ownership lost")
			}
			bootstrapKnown = true
		}
	}
	if e := root(); e != nil {
		return nil, e
	}
	if !goodOperation(id) {
		return nil, fmt.Errorf("invalid transaction ID")
	}
	if _, e := Installed(); e != nil {
		if !initial || !os.IsNotExist(e) {
			return nil, e
		}
		fresh = true
	}
	if e := trusted(rollbackDir); e != nil {
		return nil, e
	}
	if e := os.Mkdir(rollbackDir, 0700); e != nil && (!bootstrapKnown || !os.IsExist(e)) {
		return nil, fmt.Errorf("unresolved management rollback: %w", e)
	}
	var binary, cfg []byte
	var e error
	if !fresh {
		binary, e = readProtected(binaryPath(), 256<<20)
		if e != nil {
			return nil, e
		}
		cfg, e = readProtected(ConfigPath, 1<<20)
		if e != nil {
			return nil, e
		}
	} else if _, err := os.Lstat(binaryPath()); err == nil && !bootstrapKnown {
		return nil, fmt.Errorf("refusing existing unjournaled service executable")
	}
	next, e := readProtected(executable, 256<<20)
	if e != nil {
		return nil, e
	}
	t := &Transaction{Enrollment: initial, NewInstall: fresh, Version: 1, Operation: id, Phase: "prepared", Running: ServiceRunning(), Revision: revision, BinaryHash: digest(binary), ConfigHash: digest(cfg), NewBinaryHash: digest(next)}
	if fresh {
		t.BinaryHash = ""
		if bootstrapKnown {
			if old, err := readProtected(binaryPath(), 256<<20); err == nil {
				t.BinaryHash = digest(old)
			}
		}
		t.ConfigHash = ""
		t.Running = false
	}
	for name, data := range map[string][]byte{"raymond": binary, "config.yaml": cfg} {
		if fresh {
			continue
		}
		if e = control.AtomicWrite(filepath.Join(rollbackDir, name), data, func() os.FileMode {
			if name == "raymond" {
				return 0700
			}
			return 0600
		}()); e != nil {
			return nil, e
		}
	}
	if len(cli) > 0 {
		t.CLIHash = digest(cli)
		t.NewCLIHash = digest(next)
		if e = control.AtomicWrite(rollbackDir+"/cli", cli, 0600); e != nil {
			return nil, e
		}
	}
	if e = t.persist(); e != nil {
		return nil, e
	}
	return t, nil
}
func LoadTransaction() (*Transaction, error) {
	b, e := readProtected(rollbackManifest, 64<<10)
	if e != nil {
		return nil, e
	}
	var t Transaction
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if e = d.Decode(&t); e != nil {
		return nil, e
	}
	var extra any
	if e = d.Decode(&extra); e != io.EOF {
		return nil, fmt.Errorf("invalid transaction document")
	}
	if t.Version != 1 || !goodOperation(t.Operation) || (!t.NewInstall && (!goodDigest(t.BinaryHash) || !goodDigest(t.ConfigHash))) || !goodDigest(t.NewBinaryHash) || (t.CLIHash != "" && (!goodDigest(t.CLIHash) || !goodDigest(t.NewCLIHash))) {
		return nil, fmt.Errorf("invalid transaction manifest")
	}
	switch t.Phase {
	case "prepared", "applied", "committed", "rolling-back", "rolled-back":
	default:
		return nil, fmt.Errorf("invalid transaction phase")
	}
	for name, hash := range map[string]string{"raymond": t.BinaryHash, "config.yaml": t.ConfigHash, "cli": t.CLIHash} {
		if hash == "" {
			continue
		}
		b, e = readProtected(filepath.Join(rollbackDir, name), 256<<20)
		if e != nil || digest(b) != hash {
			return nil, fmt.Errorf("invalid rollback snapshot %s", name)
		}
	}
	return &t, nil
}
func ApplyManaged(t *Transaction, configPath, executable string) error {
	if e := uninstallMode(true); e != nil {
		return e
	}
	if e := installSourceMode(configPath, executable, true); e != nil {
		return e
	}
	if e := controlLocked("start"); e != nil {
		return e
	}
	t.Phase = "applied"
	return t.persist()
}
func CommitManaged(t *Transaction) error { t.Phase = "committed"; return t.persist() }
func RollbackManaged(t *Transaction) error {
	t.Phase = "rolling-back"
	if e := t.persist(); e != nil {
		return e
	}
	// A crash between the replacement journal and atomic executable publication
	// leaves either the old or new executable. Only those two proven hashes may
	// reconcile the binary record before conservative proxy cleanup.
	// The executable is retained even when the proxy journal has been removed.
	// Its ownership proof must therefore be checked independently of that journal.
	if binary, err := readProtected(binaryPath(), 256<<20); err == nil {
		hash := digest(binary)
		if hash != t.BinaryHash && hash != t.NewBinaryHash {
			return fmt.Errorf("protected executable changed outside the transaction")
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	s, e := ReadState(statePath)
	if e == nil {
		b, err := readProtected(binaryPath(), 256<<20)
		if err != nil {
			return err
		}
		actual := digest(b)
		if actual != t.BinaryHash && actual != t.NewBinaryHash {
			return fmt.Errorf("protected executable changed outside the transaction")
		}
		for i, r := range s.Files {
			if r.Path == binaryPath() {
				if r.Digest != t.BinaryHash && r.Digest != t.NewBinaryHash {
					return fmt.Errorf("unknown executable journal digest")
				}
				s.Files[i].Digest = actual
			}
		}
		if e = control.AtomicJSON(statePath, s); e != nil {
			return e
		}
	} else if !os.IsNotExist(e) {
		return e
	}
	if e = uninstallMode(true); e != nil {
		return e
	}
	if t.NewInstall {
		if b, err := readProtected(binaryPath(), 256<<20); err == nil {
			actual := digest(b)
			if actual != t.NewBinaryHash && actual != t.BinaryHash {
				return fmt.Errorf("unknown bootstrap binary")
			}
			t.NewBinaryHash = actual
		} else if !os.IsNotExist(err) {
			return err
		}
		t.Phase = "rolled-back"
		return t.persist()
	}
	if e = installSourceMode(rollbackDir+"/config.yaml", rollbackDir+"/raymond", true); e != nil {
		return e
	}
	if t.Running {
		if e = controlLocked("start"); e != nil {
			return e
		}
	}
	t.Phase = "rolled-back"
	return t.persist()
}
func RollbackCLI(t *Transaction) ([]byte, error) {
	if t.CLIHash == "" {
		return nil, nil
	}
	return readProtected(rollbackDir+"/cli", 256<<20)
}
func ClearTransaction() error {
	if e := root(); e != nil {
		return e
	}
	if e := trusted(rollbackDir); e != nil {
		return e
	}
	return os.RemoveAll(rollbackDir)
}

// ManagedControl is used only while the caller holds ManagedLock.
func ManagedControl(action string) error {
	if e := root(); e != nil {
		return e
	}
	switch action {
	case "start", "stop", "restart":
		return controlLocked(action)
	default:
		return fmt.Errorf("unsupported managed control")
	}
}

// PublishBootstrap first makes the new manager code recoverable at the fixed
// executable path, while the old proxy process continues using its mapped image.
func PublishBootstrap(t *Transaction, executable string) error {
	b, e := readProtected(executable, 256<<20)
	if e != nil {
		return e
	}
	if digest(b) != t.NewBinaryHash {
		return fmt.Errorf("bootstrap checksum mismatch")
	}
	if e = trustedParent(binaryPath()); e != nil {
		return e
	}
	if e = os.MkdirAll(filepath.Dir(binaryPath()), 0755); e != nil {
		return e
	}
	if e = control.AtomicWrite(binaryPath(), b, 0755); e != nil {
		return e
	}
	s, e := ReadState(statePath)
	if os.IsNotExist(e) && t.NewInstall {
		return nil
	}
	if e != nil {
		return e
	}
	for i, r := range s.Files {
		if r.Path == binaryPath() {
			if r.Digest != t.BinaryHash && r.Digest != t.NewBinaryHash {
				return fmt.Errorf("unknown bootstrap executable ownership")
			}
			s.Files[i].Digest = t.NewBinaryHash
		}
	}
	return control.AtomicJSON(statePath, s)
}

func PreviousConfig(t *Transaction) ([]byte, error) {
	b, e := readProtected(rollbackDir+"/config.yaml", 1<<20)
	if e != nil {
		return nil, e
	}
	if digest(b) != t.ConfigHash {
		return nil, fmt.Errorf("rollback config checksum mismatch")
	}
	return b, nil
}

func ReadManagedConfig() ([]byte, error) { return readProtected(ConfigPath, 1<<20) }
