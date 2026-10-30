package main

import (
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/cbspire/raymond/internal/config"
)

// Create the user's editable config without root privileges, including when
// installation was invoked with sudo. The service installation stays privileged.
func initializeUserConfig() (string, bool, error) {
	if os.Geteuid() != 0 || os.Getenv("SUDO_UID") == "" || os.Getenv("SUDO_UID") == "0" {
		return config.InitUser()
	}
	p, e := config.UserPath()
	if e != nil {
		return "", false, e
	}
	u, e := user.LookupId(os.Getenv("SUDO_UID"))
	if e != nil {
		return p, false, e
	}
	uid, e := strconv.ParseUint(u.Uid, 10, 32)
	if e != nil {
		return p, false, e
	}
	gid, e := strconv.ParseUint(u.Gid, 10, 32)
	if e != nil {
		return p, false, e
	}
	exe, e := os.Executable()
	if e != nil {
		return p, false, e
	}
	cmd := exec.Command(exe, "_init-user-config")
	cmd.Env = append(os.Environ(), "HOME="+filepath.Dir(filepath.Dir(p)))
	cmd.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: uint32(uid), Gid: uint32(gid)}}
	out, e := cmd.CombinedOutput()
	if e != nil {
		return p, false, fmt.Errorf("initialize invoking user's config: %w: %s", e, strings.TrimSpace(string(out)))
	}
	created, e := strconv.ParseBool(strings.TrimSpace(string(out)))
	if e != nil {
		return p, false, fmt.Errorf("invalid user config helper result: %w", e)
	}
	return p, created, nil
}
