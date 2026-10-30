package install

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/cbspire/raymond/internal/config"
)

// Setup updates a verified installation under one lifecycle lock. A saved copy
// of the old executable and protected config permits recovery after setup fails.
func Setup(path string) error {
	exe, e := os.Executable()
	if e != nil {
		return e
	}
	return SetupSource(path, exe)
}

// SetupSource applies a verified executable from a fixed upgrade staging area.
func SetupSource(path, exe string) error {
	if e := root(); e != nil {
		return e
	}
	if _, e := config.Load(path); e != nil {
		return e
	}
	owner, e := installerUID()
	if e != nil {
		return e
	}
	if _, e = readInstallExecutable(exe, owner); e != nil {
		return e
	}
	unlock, e := lifecycleLock()
	if e != nil {
		return e
	}
	defer unlock()
	_, e = Installed()
	if os.IsNotExist(e) {
		if e = installSourceLocked(path, exe); e != nil {
			return e
		}
		return controlLocked("start")
	}
	if e != nil {
		return e
	}
	backup, e := os.MkdirTemp(filepath.Dir(statePath), "upgrade-")
	if e != nil {
		return e
	}
	oldExe, oldConfig := filepath.Join(backup, "raymond"), filepath.Join(backup, "config.yaml")
	for _, f := range []struct {
		from, to string
		mode     os.FileMode
	}{{binaryPath(), oldExe, 0700}, {ConfigPath, oldConfig, 0600}} {
		b, err := os.ReadFile(f.from)
		if err == nil {
			err = os.WriteFile(f.to, b, f.mode)
		}
		if err != nil {
			_ = os.RemoveAll(backup)
			return err
		}
	}
	if e = uninstallLocked(); e != nil {
		_ = os.RemoveAll(backup)
		return e
	}
	e = installSourceLocked(path, exe)
	if e == nil {
		e = controlLocked("start")
	}
	if e == nil {
		return os.RemoveAll(backup)
	}
	original := e
	recovery := uninstallLocked()
	if recovery == nil {
		recovery = installSourceLocked(oldConfig, oldExe)
	}
	if recovery == nil {
		recovery = controlLocked("start")
	}
	if recovery != nil {
		return errors.Join(original, fmt.Errorf("recovery failed; previous binary/config retained in %s: %w", backup, recovery))
	}
	_ = os.RemoveAll(backup)
	return fmt.Errorf("setup failed; previous service restored: %w", original)
}
