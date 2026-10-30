// Package install manages an exclusive, journaled set of Raymond-owned files.
package install

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/cbspire/raymond/internal/control"
	"os"
	"path/filepath"
	"runtime"
	"sort"

	darwin "github.com/cbspire/raymond/internal/platform/darwin"
	linux "github.com/cbspire/raymond/internal/platform/linux"
)

type Record struct {
	Path   string `json:"path"`
	Digest string `json:"sha256"`
}
type State struct {
	Files   []Record `json:"files"`
	Aliases []string `json:"aliases,omitempty"`
}

func digest(b []byte) string { s := sha256.Sum256(b); return hex.EncodeToString(s[:]) }

// SafePath rejects symbolic links and writable ancestors before privileged writes.
func SafePath(path string) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return fmt.Errorf("unsafe path %s", path)
	}
	for p := path; p != "/"; p = filepath.Dir(p) {
		st, e := os.Lstat(p)
		if os.IsNotExist(e) {
			continue
		}
		if e != nil {
			return e
		}
		if runtime.GOOS == "darwin" && (p == "/etc" || p == "/var" || p == "/tmp") && st.Mode()&os.ModeSymlink != 0 {
			target, e := os.Readlink(p)
			if e == nil && (target == "private"+p || target == "/private"+p) {
				continue
			}
		}
		if st.Mode()&os.ModeSymlink != 0 || (st.Mode().Perm()&0022 != 0 && st.Mode()&os.ModeSticky == 0) {
			return fmt.Errorf("unsafe symlink or writable path %s", p)
		}
	}
	return nil
}
func ReadState(path string) (State, error) {
	var s State
	if e := SafePath(path); e != nil {
		return s, e
	}
	b, e := os.ReadFile(path)
	if e != nil {
		return s, e
	}
	e = json.Unmarshal(b, &s)
	return s, e
}
func verify(r Record) error {
	b, e := os.ReadFile(r.Path)
	if os.IsNotExist(e) {
		return nil
	}
	if e != nil {
		return e
	}
	if e = SafePath(r.Path); e != nil {
		return e
	}
	if digest(b) != r.Digest {
		return fmt.Errorf("refusing to modify changed or unowned file %s", r.Path)
	}
	return nil
}

// Apply never overwrites an existing resource. Its journal precedes file creation,
// so interrupted installations can be explicitly uninstalled without guessing.
func Apply(statePath string, files map[string][]byte, aliases []string) error {
	if _, e := os.Lstat(statePath); e == nil {
		return fmt.Errorf("installation journal exists; verify status or uninstall before reinstalling")
	} else if !os.IsNotExist(e) {
		return e
	}
	paths := make([]string, 0, len(files))
	for p := range files {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	s := State{Aliases: aliases}
	for _, p := range paths {
		if e := SafePath(p); e != nil {
			return e
		}
		if _, e := os.Lstat(p); !os.IsNotExist(e) {
			return fmt.Errorf("refusing existing resource %s", p)
		}
		s.Files = append(s.Files, Record{p, digest(files[p])})
	}
	if e := SafePath(statePath); e != nil {
		return e
	}
	if e := os.MkdirAll(filepath.Dir(statePath), 0700); e != nil {
		return e
	}
	b, e := json.MarshalIndent(s, "", "  ")
	if e != nil {
		return e
	}
	if e = exclusive(statePath, b, 0600); e != nil {
		return e
	}
	for _, p := range paths {
		if e = os.MkdirAll(filepath.Dir(p), 0755); e != nil {
			return e
		}
		mode := resourceMode(p)
		if e = exclusive(p, files[p], mode); e != nil {
			return fmt.Errorf("partial installation; uninstall to roll back: %w", e)
		}
	}
	return nil
}
func exclusive(path string, b []byte, mode os.FileMode) error {
	f, e := os.CreateTemp(filepath.Dir(path), ".raymond-*")
	if e != nil {
		return e
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if e = f.Chmod(mode); e != nil {
		f.Close()
		return e
	}
	if _, e = f.Write(b); e != nil {
		f.Close()
		return e
	}
	if e = f.Sync(); e != nil {
		f.Close()
		return e
	}
	if e = f.Close(); e != nil {
		return e
	}
	// Linking publishes a fully written file atomically and refuses an existing name.
	if e = os.Link(tmp, path); e != nil {
		return e
	}
	d, e := os.Open(filepath.Dir(path))
	if e != nil {
		return e
	}
	defer d.Close()
	return d.Sync()
}
func Verify(s State) error {
	for _, r := range s.Files {
		if e := verify(r); e != nil {
			return e
		}
	}
	return nil
}
func Remove(statePath string, s State) error {
	if e := Verify(s); e != nil {
		return e
	}
	for i := len(s.Files) - 1; i >= 0; i-- {
		if e := os.Remove(s.Files[i].Path); e != nil && !os.IsNotExist(e) {
			return e
		}
	}
	if statePath == "" {
		return nil
	}
	return os.Remove(statePath)
}

func resourceMode(path string) os.FileMode {
	if path == control.RootDir+"/owner.json" || path == control.RootDir+"/proxy-token" {
		return 0600
	}
	if path == darwin.BinaryPath || path == linux.BinaryPath {
		return 0755
	}
	return 0644
}
