package install

import (
	"bytes"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestInstallFromUserOwnedExecutable(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("reproduces an ordinary user's checkout")
	}
	p := filepath.Join(t.TempDir(), "raymond")
	want := []byte("reviewed executable snapshot")
	if e := os.WriteFile(p, want, 0755); e != nil {
		t.Fatal(e)
	}
	got, e := readInstallExecutable(p, os.Geteuid())
	if e != nil {
		t.Fatalf("sudo install rejected the invoking user's executable: %v", e)
	}
	if e := trusted(p); e == nil {
		t.Fatal("user-owned files were accepted as protected service resources")
	}
	if !bytes.Equal(got, want) {
		t.Fatal("executable bytes changed")
	}
}

func TestInstallExecutableRejectsUnsafeSources(t *testing.T) {
	for _, kind := range []string{"other-owner", "writable", "not-executable", "symlink", "symlink-parent", "directory", "fifo", "empty"} {
		t.Run(kind, func(t *testing.T) {
			dir := t.TempDir()
			p := filepath.Join(dir, "raymond")
			if e := os.WriteFile(p, []byte("executable"), 0755); e != nil {
				t.Fatal(e)
			}
			owner := os.Geteuid()
			switch kind {
			case "other-owner":
				if owner == 0 {
					t.Skip("root-owned sources remain valid for any invoking account")
				}
				owner++
			case "writable":
				if e := os.Chmod(p, 0777); e != nil {
					t.Fatal(e)
				}
			case "not-executable":
				if e := os.Chmod(p, 0644); e != nil {
					t.Fatal(e)
				}
			case "symlink":
				link := filepath.Join(dir, "link")
				if e := os.Symlink(p, link); e != nil {
					t.Fatal(e)
				}
				p = link
			case "symlink-parent":
				link := filepath.Join(t.TempDir(), "linked-directory")
				if e := os.Symlink(dir, link); e != nil {
					t.Fatal(e)
				}
				p = filepath.Join(link, "raymond")
			case "directory":
				p = dir
			case "empty":
				if e := os.Truncate(p, 0); e != nil {
					t.Fatal(e)
				}
			case "fifo":
				p = filepath.Join(dir, "pipe")
				if e := syscall.Mkfifo(p, 0700); e != nil {
					t.Fatal(e)
				}
			}
			if _, e := readInstallExecutable(p, owner); e == nil {
				t.Fatal("accepted unsafe executable source")
			}
		})
	}
}

func TestInstalledExecutableIsIndependentOfCheckout(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "source-raymond")
	want := []byte("reviewed executable")
	if e := os.WriteFile(source, want, 0755); e != nil {
		t.Fatal(e)
	}
	b, e := readInstallExecutable(source, os.Geteuid())
	if e != nil {
		t.Fatal(e)
	}
	destination := filepath.Join(dir, "managed", "raymond")
	journal := filepath.Join(dir, "state.json")
	if e := Apply(journal, map[string][]byte{destination: b}, nil); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(source, []byte("next local build"), 0755); e != nil {
		t.Fatal(e)
	}
	got, e := os.ReadFile(destination)
	if e != nil || !bytes.Equal(got, want) {
		t.Fatalf("service executable changed with checkout: %q, %v", got, e)
	}
	state, e := ReadState(journal)
	if e != nil {
		t.Fatal(e)
	}
	if e = Verify(state); e != nil {
		t.Fatal(e)
	}
}

func TestInstallerUIDIgnoresSpoofedSudoForOrdinaryUser(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("requires an ordinary invoking user")
	}
	t.Setenv("SUDO_UID", "0")
	got, e := installerUID()
	if e != nil || got != os.Geteuid() {
		t.Fatalf("trusted spoofed sudo identity: %d, %v", got, e)
	}
}

func TestSudoExecutableOwnerSelection(t *testing.T) {
	for _, tc := range []struct {
		name  string
		euid  int
		sudo  string
		want  int
		valid bool
	}{
		{"ordinary user", 501, "", 501, true},
		{"spoofed sudo ignored", 501, "0", 501, true},
		{"root shell", 0, "", 0, true},
		{"sudo user", 0, "501", 501, true},
		{"sudo root", 0, "0", 0, true},
		{"invalid sudo", 0, "invalid", 0, false},
		{"negative sudo", 0, "-1", 0, false},
		{"noncanonical sudo", 0, "0501", 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, e := sourceUID(tc.euid, tc.sudo)
			if (e == nil) != tc.valid || (e == nil && got != tc.want) {
				t.Fatalf("source owner: %d, %v", got, e)
			}
		})
	}
}
