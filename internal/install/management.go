package install

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"strconv"

	"github.com/cbspire/raymond/internal/control"
)

const managementState = control.RootDir + "/state.json"
const managementMeta = control.RootDir + "/owner.json"

func ManagementMetadata() (control.Metadata, error) {
	var m control.Metadata
	if e := trusted(managementState); e != nil {
		return m, e
	}
	s, e := ReadState(managementState)
	if e != nil {
		return m, e
	}
	for _, r := range s.Files {
		if e = trusted(r.Path); e != nil {
			return m, e
		}
		if _, e = os.Stat(r.Path); e != nil {
			return m, e
		}
	}
	if e = Verify(s); e != nil {
		return m, e
	}
	b, e := os.ReadFile(managementMeta)
	if e != nil {
		return m, e
	}
	e = json.Unmarshal(b, &m)
	return m, e
}
func PrepareManagement(path string) (control.Metadata, error) {
	var m control.Metadata
	if e := root(); e != nil {
		return m, e
	}
	unlock, e := lifecycleLock()
	if e != nil {
		return m, e
	}
	defer unlock()
	if m, e = ManagementMetadata(); e == nil {
		return m, nil
	} else if !os.IsNotExist(e) {
		return m, e
	}
	uid, e := installerUID()
	if e != nil {
		return m, e
	}
	u, e := user.LookupId(strconv.Itoa(uid))
	if e != nil {
		return m, e
	}
	gid, e := strconv.Atoi(u.Gid)
	if e != nil {
		return m, e
	}
	exe, e := os.Executable()
	if e != nil {
		return m, e
	}
	if _, e = readInstallExecutable(exe, uid); e != nil {
		return m, e
	}
	path, e = filepath.Abs(path)
	if e != nil {
		return m, e
	}
	m = control.Metadata{UID: uid, GID: gid, Home: u.HomeDir, ConfigPath: filepath.Clean(path), CLIPath: exe, OwnerToken: rand.Text(), ProxyToken: rand.Text()}
	if e = trustedParent(control.RootDir); e != nil {
		return m, e
	}
	if e = os.MkdirAll(control.RootDir, 0700); e != nil {
		return m, e
	}
	if e = os.Chmod(control.RootDir, 0700); e != nil {
		return m, e
	}
	b, e := json.Marshal(m)
	if e != nil {
		return m, e
	}
	files := map[string][]byte{managementMeta: b, control.RootDir + "/proxy-token": []byte(m.ProxyToken)}
	for p, b := range managementFiles() {
		files[p] = b
	}
	for p := range files {
		if e = trustedParent(p); e != nil {
			return m, e
		}
	}
	if e = Apply(managementState, files, nil); e != nil {
		return m, e
	}
	return m, nil
}
func managementFiles() map[string][]byte {
	if runtime.GOOS == "darwin" {
		return map[string][]byte{"/Library/LaunchDaemons/dev.raymond.manager.plist": []byte(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict><key>Label</key><string>dev.raymond.manager</string><key>ProgramArguments</key><array><string>` + binaryPath() + `</string><string>_manager</string></array><key>RunAtLoad</key><true/><key>KeepAlive</key><true/><key>AbandonProcessGroup</key><true/><key>StandardOutPath</key><string>/var/log/raymond-manager.log</string><key>StandardErrorPath</key><string>/var/log/raymond-manager.log</string></dict></plist>
`)}
	}
	return map[string][]byte{"/etc/systemd/system/raymond-manager.service": []byte(`[Unit]
Description=Raymond local management
After=network.target

[Service]
Type=simple
ExecStart=` + binaryPath() + ` _manager
Restart=on-failure
RestartSec=2
KillMode=process
UMask=0077
NoNewPrivileges=yes
PrivateTmp=yes
ProtectHome=read-only

[Install]
WantedBy=multi-user.target
`)}
}
func StartManagement(restart bool) error {
	if e := root(); e != nil {
		return e
	}
	if _, e := ManagementMetadata(); e != nil {
		return e
	}
	if runtime.GOOS == "linux" {
		if e := native("systemctl", "daemon-reload"); e != nil {
			return e
		}
		if e := native("systemctl", "enable", "raymond-manager.service"); e != nil {
			return e
		}
		action := "start"
		if restart {
			action = "restart"
		}
		return native("systemctl", action, "raymond-manager.service")
	}
	if probeNative("launchctl", "print", "system/dev.raymond.manager") == nil {
		if restart {
			return native("launchctl", "kickstart", "-k", "system/dev.raymond.manager")
		}
		return nil
	}
	return native("launchctl", "bootstrap", "system", "/Library/LaunchDaemons/dev.raymond.manager.plist")
}
func uninstallManagementLocked() error {
	_, e := ManagementMetadata()
	if os.IsNotExist(e) {
		return nil
	}
	if e != nil {
		return e
	}
	s, e := ReadState(managementState)
	if e != nil {
		return e
	}
	if runtime.GOOS == "linux" {
		if e = native("systemctl", "disable", "--now", "raymond-manager.service"); e != nil {
			return e
		}
	} else if probeNative("launchctl", "print", "system/dev.raymond.manager") == nil {
		if e = native("launchctl", "bootout", "system/dev.raymond.manager"); e != nil {
			return e
		}
	}
	if e = Remove(managementState, s); e != nil {
		return e
	}
	// The root-only directory contains sessions, operation records and rollback data,
	// all created by the broker. User config and downloaded CLI are never removed.
	if e = trusted(control.RootDir); e != nil {
		return e
	}
	if e = os.RemoveAll(control.RootDir); e != nil {
		return e
	}
	if e = trusted(control.SocketDir); e != nil {
		return e
	}
	if e = os.RemoveAll(control.SocketDir); e != nil {
		return e
	}
	if runtime.GOOS == "linux" {
		return native("systemctl", "daemon-reload")
	}
	return nil
}
func Trusted(path string) error { return trusted(path) }
func ManagedBinary() string     { return binaryPath() }
func ServiceRunning() bool {
	if runtime.GOOS == "darwin" {
		return probeNative("launchctl", "print", "system/dev.raymond") == nil
	}
	return probeNative("systemctl", "is-active", "--quiet", "raymond.service") == nil
}
func ManagementAvailable() bool { _, e := os.Stat(managementState); return e == nil }
func CheckManagementOwner(uid int) error {
	m, e := ManagementMetadata()
	if e != nil {
		return e
	}
	if uid != 0 && m.UID != uid {
		return fmt.Errorf("management belongs to another local account")
	}
	return nil
}
