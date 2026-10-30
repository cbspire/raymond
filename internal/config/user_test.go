package config

import (
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
)

func TestInitUserConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SUDO_UID", "")
	want := filepath.Join(home, ".raymond", "raymond.yaml")
	if got, e := UserPath(); e != nil || got != want {
		t.Fatalf("user path: %q, %v", got, e)
	}
	p, created, e := InitUser()
	if e != nil || !created || p != want {
		t.Fatalf("first install: %q, %v, %v", p, created, e)
	}
	if _, e = Load(p); e != nil {
		t.Fatalf("generated config is invalid: %v", e)
	}
	for path, mode := range map[string]os.FileMode{filepath.Dir(p): 0700, p: 0600} {
		st, e := os.Stat(path)
		if e != nil || st.Mode().Perm() != mode {
			t.Fatalf("permissions for %s: %v, %v", path, st, e)
		}
	}
	original, e := os.ReadFile(p)
	if e != nil {
		t.Fatal(e)
	}
	edited := append(original, []byte("# keep my settings\n")...)
	if e = os.WriteFile(p, edited, 0600); e != nil {
		t.Fatal(e)
	}
	if _, created, e = InitUser(); e != nil || created {
		t.Fatalf("repeat install: %v, %v", created, e)
	}
	got, e := os.ReadFile(p)
	if e != nil || string(got) != string(edited) {
		t.Fatalf("existing config changed: %q, %v", got, e)
	}
}

func TestInitUserConfigRejectsSymlinks(t *testing.T) {
	for _, name := range []string{"directory", "file"} {
		t.Run(name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("SUDO_UID", "")
			outside := t.TempDir()
			link, target := filepath.Join(home, ".raymond"), outside
			if name == "file" {
				if e := os.Mkdir(link, 0700); e != nil {
					t.Fatal(e)
				}
				link = filepath.Join(link, "raymond.yaml")
				target = filepath.Join(outside, "settings.yaml")
				if e := os.WriteFile(target, []byte("leave unchanged"), 0600); e != nil {
					t.Fatal(e)
				}
			}
			if e := os.Symlink(target, link); e != nil {
				t.Fatal(e)
			}
			if _, _, e := InitUser(); e == nil {
				t.Fatal("accepted a symlink")
			}
			if name == "file" {
				b, e := os.ReadFile(target)
				if e != nil || string(b) != "leave unchanged" {
					t.Fatalf("symlink target changed: %q, %v", b, e)
				}
			} else if entries, e := os.ReadDir(outside); e != nil || len(entries) != 0 {
				t.Fatalf("wrote through directory symlink: %v, %v", entries, e)
			}
		})
	}
}

func TestUserConfigSudoHome(t *testing.T) {
	uid := strconv.Itoa(os.Getuid())
	account, e := user.LookupId(uid)
	if e != nil {
		t.Fatal(e)
	}
	owner, e := resolveUser(0, uid, "/root")
	if e != nil || owner.home != account.HomeDir || strconv.Itoa(owner.uid) != uid {
		t.Fatalf("sudo used wrong home/account: %+v, %v", owner, e)
	}
	owner, e = resolveUser(os.Getuid()+1, uid, "/ordinary-user")
	if e != nil || owner.home != "/ordinary-user" {
		t.Fatalf("non-root trusted SUDO_UID: %+v, %v", owner, e)
	}
	if _, e = resolveUser(0, "not-a-uid", "/root"); e == nil {
		t.Fatal("invalid sudo identity fell back to root")
	}
	if _, e = resolveUser(os.Getuid(), "", "relative-home"); e == nil {
		t.Fatal("accepted relative home")
	}
}

func TestInitUserConfigRefusesDifferentIdentity(t *testing.T) {
	home := t.TempDir()
	if _, e := initUser(configOwner{home: home, uid: os.Geteuid() + 1}); e == nil {
		t.Fatal("created user config with another user's privileges")
	}
	if entries, e := os.ReadDir(home); e != nil || len(entries) != 0 {
		t.Fatalf("wrote files as a different user: %v, %v", entries, e)
	}
}

func TestConcurrentUserConfigInitialization(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SUDO_UID", "")
	var wg sync.WaitGroup
	created := make(chan bool, 8)
	for range 8 {
		wg.Go(func() {
			p, first, e := InitUser()
			if e != nil {
				t.Error(e)
				return
			}
			if _, e = Load(p); e != nil {
				t.Errorf("read partial configuration: %v", e)
			}
			created <- first
		})
	}
	wg.Wait()
	close(created)
	count := 0
	for first := range created {
		if first {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("expected one atomic creation, got %d", count)
	}
	entries, e := os.ReadDir(filepath.Join(home, ".raymond"))
	if e != nil || len(entries) != 1 || entries[0].Name() != "raymond.yaml" {
		t.Fatalf("temporary files left behind: %v, %v", entries, e)
	}
}

func TestInitUserConfigPreservesInvalidFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SUDO_UID", "")
	if e := os.Mkdir(filepath.Join(home, ".raymond"), 0700); e != nil {
		t.Fatal(e)
	}
	p := filepath.Join(home, ".raymond", "raymond.yaml")
	if e := os.WriteFile(p, []byte("my invalid config"), 0600); e != nil {
		t.Fatal(e)
	}
	if _, _, e := InitUser(); e == nil || !strings.Contains(e.Error(), "config") {
		t.Fatalf("invalid existing config was accepted: %v", e)
	}
	b, e := os.ReadFile(p)
	if e != nil || string(b) != "my invalid config" {
		t.Fatalf("invalid file was overwritten: %q, %v", b, e)
	}
}
