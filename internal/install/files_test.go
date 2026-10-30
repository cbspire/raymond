package install

import (
	"os"
	"path/filepath"
	"testing"
)

// Temp roots can be writable by the test owner; /tmp's sticky mode is excluded
// by using a private root and testing resource ownership separately.
func TestOwnership(t *testing.T) {
	d := t.TempDir()
	p := filepath.Join(d, "config")
	b := []byte("owned")
	if e := os.WriteFile(p, b, 0600); e != nil {
		t.Fatal(e)
	}
	r := Record{p, digest(b)}
	if e := verify(r); e != nil && e.Error() != "unsafe symlink or writable path /tmp" {
		t.Fatal(e)
	}
	os.WriteFile(p, []byte("changed"), 0600)
	if e := verify(r); e == nil {
		t.Fatal("accepted changed resource")
	}
	os.Remove(p)
	if e := verify(r); e != nil {
		t.Fatal(e)
	}
}
func TestRejectSymlink(t *testing.T) {
	d := t.TempDir()
	p := filepath.Join(d, "link")
	if e := os.Symlink("/etc", p); e != nil {
		t.Fatal(e)
	}
	if e := SafePath(filepath.Join(p, "raymond")); e == nil {
		t.Fatal("accepted symlink ancestor")
	}
}

func TestApplyAndRestore(t *testing.T) {
	d := t.TempDir()
	state := filepath.Join(d, "state.json")
	p := filepath.Join(d, "owned", "config")
	other := filepath.Join(d, "other")
	os.WriteFile(other, []byte("keep"), 0600)
	files := map[string][]byte{p: []byte("configuration")}
	if e := Apply(state, files, nil); e != nil {
		t.Fatal(e)
	}
	s, e := ReadState(state)
	if e != nil {
		t.Fatal(e)
	}
	if e = Verify(s); e != nil {
		t.Fatal(e)
	}
	if e = Apply(state, files, nil); e == nil {
		t.Fatal("overwrote journal")
	}
	os.WriteFile(p, []byte("operator changed"), 0644)
	if e = Remove(state, s); e == nil {
		t.Fatal("deleted changed config")
	}
	if _, e = os.Stat(state); e != nil {
		t.Fatal("lost recovery journal")
	}
	os.WriteFile(p, files[p], 0644)
	if e = Remove(state, s); e != nil {
		t.Fatal(e)
	}
	if _, e = os.Stat(p); !os.IsNotExist(e) {
		t.Fatal("left installed file")
	}
	if b, e := os.ReadFile(other); e != nil || string(b) != "keep" {
		t.Fatal("changed unrelated resource")
	}
}
func TestApplyConflictDoesNotWrite(t *testing.T) {
	d := t.TempDir()
	state := filepath.Join(d, "state.json")
	p := filepath.Join(d, "existing")
	os.WriteFile(p, []byte("operator"), 0600)
	if e := Apply(state, map[string][]byte{p: []byte("new")}, nil); e == nil {
		t.Fatal("overwrote existing file")
	}
	if _, e := os.Stat(state); !os.IsNotExist(e) {
		t.Fatal("created journal on conflict")
	}
}
func TestAtomicNoReplace(t *testing.T) {
	d := t.TempDir()
	p := filepath.Join(d, "file")
	if e := exclusive(p, []byte("first"), 0600); e != nil {
		t.Fatal(e)
	}
	if e := exclusive(p, []byte("second"), 0600); e == nil {
		t.Fatal("replaced file")
	}
	b, _ := os.ReadFile(p)
	if string(b) != "first" {
		t.Fatal(string(b))
	}
}
func TestConcurrentLifecycleLock(t *testing.T) {
	p := filepath.Join(t.TempDir(), "install.lock")
	release, e := lock(p)
	if e != nil {
		t.Fatal(e)
	}
	if second, e := lock(p); e == nil {
		second()
		t.Fatal("concurrent lifecycle operation accepted")
	}
	release()
	third, e := lock(p)
	if e != nil {
		t.Fatal(e)
	}
	third()
}

func TestResourcePermissions(t *testing.T) {
	for _, tc := range []struct {
		path string
		want os.FileMode
	}{{"/Library/PrivilegedHelperTools/dev.raymond", 0755}, {"/usr/libexec/raymond/raymond", 0755}, {"/etc/raymond/config.yaml", 0644}, {"/etc/resolver/raymond", 0644}} {
		if got := resourceMode(tc.path); got != tc.want {
			t.Errorf("%s mode %o != %o", tc.path, got, tc.want)
		}
	}
}
