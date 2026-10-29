//go:build osintegration && (darwin || linux)

package acceptance

import (
	"encoding/json"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// candidate installs a real GoReleaser archive as an ordinary user without Go,
// curl or any network access. Only its resulting single native binary is used.
func candidate(t *testing.T) string {
	t.Helper()
	requireDisposable(t)
	assets := os.Getenv("RAYMOND_ARCHIVE_DIR")
	script := os.Getenv("RAYMOND_INSTALL_SCRIPT")
	testUser := os.Getenv("RAYMOND_TEST_USER")
	if !filepath.IsAbs(assets) || !filepath.IsAbs(script) || testUser == "" || testUser == "root" {
		t.Fatal("require immutable candidate archives, installer script and non-root acceptance user")
	}
	var metadata struct {
		Version string `json:"version"`
	}
	b, e := os.ReadFile(filepath.Join(assets, "metadata.json"))
	if e != nil {
		t.Fatal(e)
	}
	if e = json.Unmarshal(b, &metadata); e != nil || metadata.Version == "" {
		t.Fatal("invalid GoReleaser candidate metadata", e)
	}
	account, e := user.Lookup(testUser)
	if e != nil {
		t.Fatal(e)
	}
	uid, e := strconv.Atoi(account.Uid)
	if e != nil || uid == 0 {
		t.Fatal("acceptance user must not be root")
	}
	gid, e := strconv.Atoi(account.Gid)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.MkdirAll("/var/lib", 0755); e != nil {
		t.Fatal(e)
	}
	root, e := os.MkdirTemp("/var/lib", "raymond-package-")
	if e != nil {
		t.Fatal(e)
	}
	if e = os.Chmod(root, 0755); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	dir := filepath.Join(root, "user-bin")
	if e = os.Mkdir(dir, 0700); e != nil {
		t.Fatal(e)
	}
	if e = os.Chown(dir, uid, gid); e != nil {
		t.Fatal(e)
	}
	// The environment has no Go on PATH, and local archive mode must not run curl.
	if _, e = exec.LookPath("go"); e == nil {
		t.Fatal("Go unexpectedly available in runtime PATH")
	}
	out := must(t, nativePath("sudo"), "-H", "-u", testUser, "--", nativePath("env"), "PATH="+os.Getenv("PATH"), "TMPDIR="+dir, "RAYMOND_ARCHIVE_DIR="+assets, "RAYMOND_VERSION=v"+metadata.Version, "RAYMOND_INSTALL_DIR="+dir, nativePath("bash"), script)
	t.Logf("native packaged binary installed without Go: %s", strings.TrimSpace(string(out)))
	binary := filepath.Join(dir, "raymond")
	st, e := os.Stat(binary)
	if e != nil || st.Mode().Perm() != 0755 {
		t.Fatal("incorrect installed executable mode", e)
	}
	version := must(t, binary, "version")
	if !strings.Contains(string(version), metadata.Version) {
		t.Fatal("binary version does not match candidate archive", string(version))
	}
	noRuntimeDependencies(t, binary)
	return binary
}
