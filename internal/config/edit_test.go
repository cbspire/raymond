package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOwnedEditPreservesCommentsAndRejectsStaleWrites(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.yaml")
	old := []byte("# My local projects\nversion: 1\ndns:\n  zones: [test] # Private DNS\ninstallations:\n  # Application notes\n  - domain: app.test\n    target: 127.0.0.1:9001 # Backend\n")
	if e := os.WriteFile(p, old, 0600); e != nil {
		t.Fatal(e)
	}
	c, e := Load(p)
	if e != nil {
		t.Fatal(e)
	}
	c.Routes[0].Target = "127.0.0.1:9002"
	revision, e := SaveOwned(p, Revision(old), c)
	if e != nil {
		t.Fatal(e)
	}
	b, e := OwnedDocument(p)
	if e != nil {
		t.Fatal(e)
	}
	for _, s := range []string{"My local projects", "Private DNS", "Application notes", "Backend", "9002"} {
		if !strings.Contains(string(b), s) {
			t.Fatalf("lost %s: %s", s, b)
		}
	}
	if revision != Revision(b) {
		t.Fatal("wrong revision")
	}
	if _, e = SaveOwned(p, Revision(old), c); e == nil {
		t.Fatal("accepted stale config")
	}
	link := filepath.Join(filepath.Dir(p), "link.yaml")
	if e = os.Symlink(p, link); e != nil {
		t.Fatal(e)
	}
	if _, e = OwnedDocument(link); e == nil {
		t.Fatal("followed config symlink")
	}
}

func TestDiscardInvalidOwnedConfigRequiresCurrentRevision(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	invalid := []byte("version: [broken yaml\n")
	if e := os.WriteFile(path, invalid, 0600); e != nil {
		t.Fatal(e)
	}
	active := Config{Version: 1, DNS: DNS{Zones: []string{"test"}}}
	if e := active.Validate(); e != nil {
		t.Fatal(e)
	}
	if _, e := SaveOwned(path, Revision(invalid), active); e == nil {
		t.Fatal("ordinary save discarded invalid YAML implicitly")
	}
	if _, e := DiscardOwned(path, strings.Repeat("0", 64), active); e == nil {
		t.Fatal("discard ignored external revision")
	}
	if _, e := DiscardOwned(path, Revision(invalid), active); e != nil {
		t.Fatal(e)
	}
	if _, e := Load(path); e != nil {
		t.Fatal("discard did not restore valid config", e)
	}
}
