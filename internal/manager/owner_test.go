package manager

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/cbspire/raymond/internal/control"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cbspire/raymond/internal/config"
)

func helperTest(t *testing.T, r OwnerRequest) (OwnerResult, error) {
	t.Helper()
	b, e := json.Marshal(r)
	if e != nil {
		t.Fatal(e)
	}
	var out bytes.Buffer
	e = OwnerHelper(bytes.NewReader(b), &out)
	var result OwnerResult
	if e == nil {
		e = json.Unmarshal(out.Bytes(), &result)
	}
	return result, e
}
func TestOwnerHelperConfigAndCredentialSafety(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("ordinary-user helper")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	path := filepath.Join(home, "config.yaml")
	old := []byte("# Keep my comment\nversion: 1\ndns:\n  zones: [test]\n")
	if e := os.WriteFile(path, old, 0600); e != nil {
		t.Fatal(e)
	}
	read, e := helperTest(t, OwnerRequest{Action: "config-read", Path: path})
	if e != nil || read.Revision != config.Revision(old) {
		t.Fatal(e)
	}
	c, e := config.Decode(bytes.NewReader(old))
	if e != nil {
		t.Fatal(e)
	}
	c.Routes = append(c.Routes, config.Route{Domain: "*.apps.test", Target: "127.0.0.1:3000"})
	if _, e = helperTest(t, OwnerRequest{Action: "config-save", Path: path, Config: &c, Revision: read.Revision}); e != nil {
		t.Fatal(e)
	}
	if _, e = helperTest(t, OwnerRequest{Action: "config-save", Path: path, Config: &c, Revision: read.Revision}); e == nil {
		t.Fatal("stale helper save accepted")
	}
	doc, e := os.ReadFile(path)
	if e != nil || !strings.Contains(string(doc), "Keep my comment") {
		t.Fatal(e)
	}
	link := filepath.Join(home, "link")
	if e = os.Symlink(path, link); e != nil {
		t.Fatal(e)
	}
	if _, e = helperTest(t, OwnerRequest{Action: "config-read", Path: link}); e == nil {
		t.Fatal("helper followed symlink")
	}
	if _, e = helperTest(t, OwnerRequest{Action: "credential", Path: "/ignored", Data: []byte(`{"token":"private"}`)}); e != nil {
		t.Fatal(e)
	}
	credential := filepath.Join(home, ".raymond", "control.json")
	st, e := os.Stat(credential)
	if e != nil || st.Mode().Perm() != 0600 {
		t.Fatal("credential permissions", e)
	}
	if e = os.Chmod(credential, 0644); e != nil {
		t.Fatal(e)
	}
	if _, e = helperTest(t, OwnerRequest{Action: "credential", Data: []byte("secret")}); e == nil {
		t.Fatal("replaced public credential")
	}
}
func TestOwnerCLIReplacementCAS(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("ordinary owner")
	}
	path := filepath.Join(t.TempDir(), "raymond")
	if e := os.WriteFile(path, []byte("old executable"), 0755); e != nil {
		t.Fatal(e)
	}
	before, e := helperTest(t, OwnerRequest{Action: "cli-read", Path: path})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = helperTest(t, OwnerRequest{Action: "cli-save", Path: path, Revision: "stale", Data: []byte("bad")}); e == nil {
		t.Fatal("replaced modified CLI")
	}
	if _, e = helperTest(t, OwnerRequest{Action: "cli-save", Path: path, Revision: before.Revision, Data: []byte("new executable")}); e != nil {
		t.Fatal(e)
	}
	got, e := os.ReadFile(path)
	if e != nil || string(got) != "new executable" {
		t.Fatal(e)
	}
}

func TestOwnerCallRejectsUnrepresentableIdentity(t *testing.T) {
	for _, ids := range [][2]int{{0, 1}, {-1, 1}, {1, -1}, {math.MaxUint32, 1}, {math.MaxUint32 + 1, 1}, {1, math.MaxUint32}, {1, math.MaxUint32 + 1}} {
		_, err := EnrollOwner(context.Background(), "/nonexistent-raymond-helper", control.Metadata{UID: ids[0], GID: ids[1]}, OwnerRequest{Action: "config-read"})
		if err == nil || !strings.Contains(err.Error(), "invalid enrolled account") {
			t.Fatalf("identity %v reached helper execution: %v", ids, err)
		}
	}
}
