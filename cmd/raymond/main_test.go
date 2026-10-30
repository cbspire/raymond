package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cbspire/raymond/internal/config"
)

func TestInvalidArguments(t *testing.T) {
	for _, args := range [][]string{nil, {"unknown"}, {"version", "extra"}, {"config"}, {"config", "validate", "--bad", "file"}, {"serve", "--config"}, {"start", "extra"}, {"uninstall", "extra"}, {"setup", "--bad"}, {"upgrade", "extra"}, {"update", "--version", "main"}} {
		if e := run(args); e == nil {
			t.Errorf("accepted %v", args)
		}
	}
}

func TestInstallCreatesDefaultUserConfig(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("ordinary-user install must not install OS resources")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SUDO_UID", "")
	if e := run([]string{"install"}); e != nil {
		t.Fatal(e)
	}
	p := filepath.Join(home, ".raymond", "raymond.yaml")
	if _, e := config.Load(p); e != nil {
		t.Fatalf("install did not generate a valid user config: %v", e)
	}
	if e := run([]string{"config", "validate"}); e != nil {
		t.Fatalf("validate did not default to user config: %v", e)
	}
	if e := run([]string{"install"}); e != nil {
		t.Fatalf("repeat install: %v", e)
	}
}

func TestExplicitInstallConfigDoesNotCreateDefault(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("ordinary-user install must not install OS resources")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	p := filepath.Join(home, "custom.yaml")
	if e := os.WriteFile(p, []byte("version: 1\ndns:\n  zones: [custom.test]\n"), 0600); e != nil {
		t.Fatal(e)
	}
	if e := run([]string{"install", "--config", p}); e != nil {
		t.Fatal(e)
	}
	if _, e := os.Stat(filepath.Join(home, ".raymond")); !os.IsNotExist(e) {
		t.Fatalf("explicit config created a default directory: %v", e)
	}
}
func TestVersionAndConfigPath(t *testing.T) {
	for _, args := range [][]string{{"version"}, {"config", "path"}} {
		if e := run(args); e != nil {
			t.Fatal(e)
		}
	}
}
