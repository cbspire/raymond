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
	if e = validateManagementResources(s); e != nil {
		return m, e
	}
	if e = Verify(s); e != nil {
		return m, e
	}
	b, e := os.ReadFile(managementMeta)
	if e != nil {
		return m, e
	}
	e = json.Unmarshal(b, &m)
	if e != nil {
		return m, e
	}
	return m, validateManagementMetadata(m)
}
func PrepareManagement(path string, validators ...func(control.Metadata) error) (control.Metadata, error) {
	var m control.Metadata
	if e := root(); e != nil {
		return m, e
	}
	unlock, e := lifecycleLock()
	if e != nil {
		return m, e
	}
	defer unlock()
	uid, e := installerUID()
	if e != nil {
		return m, e
	}
	path, e = filepath.Abs(path)
	if e != nil {
		return m, e
	}
	if m, e = ManagementMetadata(); e == nil {
		if (uid != 0 && uid != m.UID) || filepath.Clean(path) != m.ConfigPath {
			return m, fmt.Errorf("existing management belongs to a different owner or configuration path")
		}
		for _, validate := range validators {
			if err := validate(m); err != nil {
				return m, err
			}
		}
		return m, nil
	} else if !os.IsNotExist(e) {
		return m, e
	}
	if uid <= 0 {
		return m, fmt.Errorf("management enrollment requires the invoking ordinary account")
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

	path, e = filepath.Abs(path)
	if e != nil {
		return m, e
	}
	m = control.Metadata{UID: uid, GID: gid, Home: u.HomeDir, ConfigPath: filepath.Clean(path), CLIPath: exe, OwnerToken: rand.Text(), ProxyToken: rand.Text()}
	for _, validate := range validators {
		if err := validate(m); err != nil {
			return m, err
		}
	}
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
	if e := trusted(binaryPath()); e != nil {
		return e
	}
	if runtime.GOOS == "linux" {
		if e := native("systemctl", "daemon-reload"); e != nil {
			return e
		}
		if e := native("systemctl", "reset-failed", "raymond-manager.service"); e != nil {
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
		return native("launchctl", "kickstart", "system/dev.raymond.manager")
	}
	return native("launchctl", "bootstrap", "system", "/Library/LaunchDaemons/dev.raymond.manager.plist")
}
func uninstallManagementLocked() error {
	if e := trusted(managementState); e != nil {
		return e
	}
	state, e := ReadState(managementState)
	if os.IsNotExist(e) {
		return nil
	}
	if e != nil {
		return e
	}
	if e = validateManagementResources(state); e != nil {
		return e
	}
	for _, record := range state.Files {
		if e = trusted(record.Path); e != nil {
			return e
		}
	}
	if e = Verify(state); e != nil {
		return e
	}
	if runtime.GOOS == "linux" {
		_, exists := os.Stat("/etc/systemd/system/raymond-manager.service")
		if exists == nil || probeNative("systemctl", "is-active", "--quiet", "raymond-manager.service") == nil {
			if e = native("systemctl", "disable", "--now", "raymond-manager.service"); e != nil {
				return e
			}
		}
	} else if probeNative("launchctl", "print", "system/dev.raymond.manager") == nil {
		if e = native("launchctl", "bootout", "system/dev.raymond.manager"); e != nil {
			return e
		}
	}
	// A failed fresh enrollment has no proxy journal yet. Its retained transaction
	// proves ownership of the bootstrap binary; never remove an unrelated executable.
	if _, err := os.Stat(statePath); os.IsNotExist(err) {
		if transaction, err := LoadTransaction(); err == nil && transaction.NewInstall {
			binary, err := readProtected(binaryPath(), 256<<20)
			if err == nil {
				hash := digest(binary)
				if hash != transaction.NewBinaryHash && hash != transaction.BinaryHash {
					return fmt.Errorf("bootstrap executable changed; refusing cleanup")
				}
				if err = os.Remove(binaryPath()); err != nil {
					return err
				}
			} else if !os.IsNotExist(err) {
				return err
			}
		} else if err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	if e = Remove(managementState, state); e != nil {
		return e
	}
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
func validateManagementResources(s State) error {
	allowed := managementFiles()
	allowed[managementMeta] = nil
	allowed[control.RootDir+"/proxy-token"] = nil
	if len(s.Files) != len(allowed) {
		return fmt.Errorf("invalid management resource journal")
	}
	for _, r := range s.Files {
		if _, ok := allowed[r.Path]; !ok {
			return fmt.Errorf("unknown management resource")
		}
		delete(allowed, r.Path)
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

func validateManagementMetadata(m control.Metadata) error {
	if m.UID <= 0 || m.GID < 0 || len(m.OwnerToken) < 26 || len(m.ProxyToken) < 26 || m.OwnerToken == m.ProxyToken {
		return fmt.Errorf("invalid management identity")
	}
	for _, path := range []string{m.Home, m.ConfigPath, m.CLIPath} {
		if !filepath.IsAbs(path) || filepath.Clean(path) != path {
			return fmt.Errorf("invalid management path")
		}
	}
	u, e := user.LookupId(strconv.Itoa(m.UID))
	if e != nil {
		return e
	}
	gid, e := strconv.Atoi(u.Gid)
	if e != nil {
		return e
	}
	if u.HomeDir != m.Home || gid != m.GID {
		return fmt.Errorf("enrolled account identity changed")
	}
	return nil
}

func ManagementReady() bool {
	if e := trusted(control.RootDir + "/ready.json"); e != nil {
		return false
	}
	b, e := os.ReadFile(control.RootDir + "/ready.json")
	if e != nil {
		return false
	}
	var ready struct {
		Version int `json:"version"`
	}
	return json.Unmarshal(b, &ready) == nil && ready.Version == 1
}
func CompleteManagementEnrollment() error {
	if e := root(); e != nil {
		return e
	}
	return control.AtomicJSON(control.RootDir+"/ready.json", map[string]int{"version": 1})
}
