//go:build osintegration && (darwin || linux)

package acceptance

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
)

// Some hosted Ubuntu images use a foreign resolv.conf despite running resolved.
// Test the production refusal first, then configure only this disposable runner
// as a supported fixture. The installer itself must never rewire resolv.conf.
func prepareResolver(t *testing.T, binary, configPath string) {
	t.Helper()
	if runtime.GOOS != "linux" {
		return
	}
	const path = "/etc/resolv.conf"
	const stub = "/run/systemd/resolve/stub-resolv.conf"
	target, e := filepath.EvalSymlinks(path)
	if e == nil && target == stub {
		return
	}
	info, e := os.Lstat(path)
	if e != nil {
		t.Fatal(e)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		t.Fatal("cannot record resolver file ownership")
	}
	var original []byte
	link := ""
	if info.Mode()&os.ModeSymlink != 0 {
		link, e = os.Readlink(path)
	} else if info.Mode().IsRegular() {
		original, e = os.ReadFile(path)
	} else {
		t.Fatal("unsupported resolver fixture file type")
	}
	if e != nil {
		t.Fatal(e)
	}
	if out, e := command(binary, "install", "--config", configPath); e == nil || !strings.Contains(string(out), "refusing to rewire the resolver") {
		t.Fatalf("foreign resolver was not rejected: %v %s", e, out)
	}
	if link != "" {
		got, e := os.Readlink(path)
		if e != nil || got != link {
			t.Fatal("installer modified foreign resolver symlink")
		}
	} else {
		got, e := os.ReadFile(path)
		if e != nil || !bytes.Equal(got, original) {
			t.Fatal("installer modified foreign resolver file")
		}
	}
	if _, e = os.Stat(stub); e != nil {
		t.Fatal("runner does not provide the supported resolved stub", e)
	}
	temp, e := os.MkdirTemp("/etc", ".raymond-resolver-fixture-")
	if e != nil {
		t.Fatal(e)
	}
	staged := filepath.Join(temp, "resolv.conf")
	if e = os.Symlink(stub, staged); e != nil {
		t.Fatal(e)
	}
	if e = os.Rename(staged, path); e != nil {
		_ = os.RemoveAll(temp)
		t.Fatalf("runner cannot prepare a reversible resolver fixture: %v", e)
	}
	t.Cleanup(func() {
		defer os.RemoveAll(temp)
		got, e := os.Readlink(path)
		if e != nil || got != stub {
			t.Errorf("Raymond changed the supported resolver fixture: %v %q", e, got)
		}
		if link != "" {
			e = os.Symlink(link, staged)
			if e == nil {
				e = os.Lchown(staged, int(stat.Uid), int(stat.Gid))
			}
		} else {
			e = os.WriteFile(staged, original, info.Mode().Perm())
			if e == nil {
				e = os.Chmod(staged, info.Mode().Perm())
			}
			if e == nil {
				e = os.Chown(staged, int(stat.Uid), int(stat.Gid))
			}
		}
		if e == nil {
			e = os.Rename(staged, path)
		}
		if e != nil {
			t.Errorf("cannot restore original runner resolver: %v", e)
			return
		}
		if link != "" {
			s, e := os.Readlink(path)
			if e != nil || s != link {
				t.Error("original resolver symlink was not restored")
			}
		} else {
			b, e := os.ReadFile(path)
			if e != nil || !bytes.Equal(b, original) {
				t.Error("original resolver file was not restored")
			}
		}
	})
	t.Logf("prepared reversible supported resolver fixture; original %s restored at cleanup", info.Mode().Type())
}
