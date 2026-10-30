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
