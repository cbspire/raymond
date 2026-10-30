package install

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/cbspire/raymond/internal/config"
	"github.com/cbspire/raymond/internal/diagnostics"
	darwin "github.com/cbspire/raymond/internal/platform/darwin"
	linux "github.com/cbspire/raymond/internal/platform/linux"
	"gopkg.in/yaml.v3"
)

const ConfigPath = "/etc/raymond/config.yaml"
const statePath = "/var/lib/raymond/state.json"

func binaryPath() string {
	if runtime.GOOS == "darwin" {
		return darwin.BinaryPath
	}
	return linux.BinaryPath
}

func root() error {
	if os.Geteuid() != 0 {
		return fmt.Errorf("administrator privileges required; explicitly rerun with sudo")
	}
	return nil
}
func native(name string, args ...string) error {
	return runNative(false, name, args...)
}

func probeNative(name string, args ...string) error {
	return runNative(true, name, args...)
}

func runNative(quiet bool, name string, args ...string) error {
	paths := map[string]string{"systemctl": "/usr/bin/systemctl", "resolvectl": "/usr/bin/resolvectl", "networkctl": "/usr/bin/networkctl", "udevadm": "/usr/bin/udevadm", "ip": "/usr/sbin/ip", "launchctl": "/bin/launchctl", "ifconfig": "/sbin/ifconfig"}
	path, ok := paths[name]
	if !ok {
		return fmt.Errorf("unsupported native command")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	c := exec.CommandContext(ctx, path, args...)
	if !quiet {
		c.Stdout = os.Stdout
		c.Stderr = os.Stderr
	}
	if e := c.Run(); e != nil {
		return fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), e)
	}
	return nil
}
func trusted(path string) error {
	return trustedFor(path, 0)
}

func trustedFor(path string, owner int) error {
	if e := SafePath(path); e != nil {
		return e
	}
	for p := path; p != "/"; p = filepath.Dir(p) {
		st, e := os.Stat(p)
		if os.IsNotExist(e) {
			continue
		}
		if e != nil {
			return e
		}
		s, ok := st.Sys().(*syscall.Stat_t)
		if !ok || (s.Uid != 0 && int(s.Uid) != owner) {
			if owner == 0 {
				return fmt.Errorf("%s must be root-owned", p)
			}
			return fmt.Errorf("%s must belong to root or the invoking user", p)
		}
	}
	return nil
}
func Installed() (State, error) {
	if e := trusted(statePath); e != nil {
		return State{}, e
	}
	s, e := ReadState(statePath)
	if e != nil {
		return s, e
	}
	for _, r := range s.Files {
		if e = trusted(r.Path); e != nil {
			return s, e
		}
	}
	return s, Verify(s)
}
func aliases() (map[string]bool, error) {
	found := map[string]bool{}
	is, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	for _, i := range is {
		as, err := i.Addrs()
		if err != nil {
			return nil, err
		}
		for _, a := range as {
			ip, _, e := net.ParseCIDR(a.String())
			if e == nil {
				found[ip.String()] = true
			}
		}
	}
	return found, nil
}
func Install(path string) error {
	if e := root(); e != nil {
		return e
	}
	unlock, e := lifecycleLock()
	if e != nil {
		return e
	}
	defer unlock()
	c, e := config.Load(path)
	if e != nil {
		return e
	}
	if c.DNS.Listen != "127.0.0.43:53" || c.Proxy.Listen != "127.0.0.42:80" {
		return fmt.Errorf("OS installation requires default addresses and ports; use serve for custom test ports")
	}
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		return fmt.Errorf("unsupported platform")
	}
	files := darwin.Files(c)
	if runtime.GOOS == "linux" {
		files = linux.Files(c)
	}
	b, e := yaml.Marshal(c)
	if e != nil {
		return e
	}
	files[ConfigPath] = b
	exe, e := os.Executable()
	if e != nil {
		return e
	}
	owner, e := installerUID()
	if e != nil {
		return e
	}
	b, e = readInstallExecutable(exe, owner)
	if e != nil {
		return e
	}
	files[binaryPath()] = b
	for p := range files {
		if e = trustedParent(p); e != nil {
			return e
		}
	}
	if e = trustedParent(statePath); e != nil {
		return e
	}
	if old, e := ReadState(statePath); e == nil {
		if e = trusted(statePath); e != nil {
			return e
		}
		if e = Verify(old); e != nil {
			return e
		}
		if len(old.Files) != len(files) {
			return fmt.Errorf("installed resource set differs; uninstall first")
		}
		for _, r := range old.Files {
			want, ok := files[r.Path]
			got, e := os.ReadFile(r.Path)
			if !ok || e != nil || !bytes.Equal(want, got) {
				return fmt.Errorf("configuration/binary changed or install incomplete; uninstall before reinstalling")
			}
		}
		return nil
	} else if !os.IsNotExist(e) {
		return e
	}
	// Detect conflicts that need no aliases before creating owned OS resources.
	if e = diagnostics.CheckWildcardListeners(c); e != nil {
		return e
	}
	var created []string
	if runtime.GOOS == "linux" {
		if e = PlatformCheck(c.DNS.Zones...); e != nil {
			return e
		}
		if _, e = net.InterfaceByName("raymond0"); e == nil {
			return fmt.Errorf("raymond0 already exists; refusing ambiguous ownership")
		}
		assigned, e := aliases()
		if e != nil {
			return e
		}
		for _, ip := range []string{"192.0.2.42", "127.0.0.43"} {
			if assigned[ip] {
				return fmt.Errorf("reserved Raymond interface address %s is already assigned; refusing to change its routing", ip)
			}
		}
	} else {
		assigned, e := aliases()
		if e != nil {
			return e
		}
		for _, a := range []string{"127.0.0.42", "127.0.0.43"} {
			if !assigned[a] {
				created = append(created, a)
			}
		}
	}
	// On macOS missing aliases cannot be bind-checked until explicitly added.
	if runtime.GOOS == "linux" {
		if e = diagnostics.Check(c); e != nil {
			return e
		}
	}
	if e = Apply(statePath, files, created); e != nil {
		return e
	}
	fail := func(e error) error {
		return fmt.Errorf("installation journal retained; run sudo raymond uninstall to roll back: %w", e)
	}
	if runtime.GOOS == "darwin" {
		if e = ensureAliasesLocked(); e != nil {
			return fail(e)
		}
		if e = diagnostics.Check(c); e != nil {
			return fail(e)
		}
	} else {
		if e = native("systemctl", "daemon-reload"); e != nil {
			return fail(e)
		}
		if e = native("udevadm", "control", "--reload"); e != nil {
			return fail(e)
		}
		if e = native("networkctl", "reload"); e != nil {
			return fail(e)
		}
		if e = checkLinuxLink(c); e != nil {
			return fail(e)
		}
		if e = native("systemctl", "enable", "raymond.service"); e != nil {
			return fail(e)
		}
	}
	return nil
}
func EnsureAliases() error {
	if runtime.GOOS != "darwin" {
		return nil
	}
	if e := root(); e != nil {
		return e
	}
	unlock, e := lifecycleLock()
	if e != nil {
		return e
	}
	defer unlock()
	return ensureAliasesLocked()
}
func ensureAliasesLocked() error {
	if runtime.GOOS != "darwin" {
		return nil
	}
	if e := root(); e != nil {
		return e
	}
	s, e := Installed()
	if e != nil {
		return e
	}
	assigned, e := aliases()
	if e != nil {
		return e
	}
	for _, a := range s.Aliases {
		if a != "127.0.0.42" && a != "127.0.0.43" {
			return fmt.Errorf("invalid alias journal")
		}
		if !assigned[a] {
			if e = native("ifconfig", "lo0", "alias", a, "up"); e != nil {
				return e
			}
		}
	}
	return nil
}
func Control(action string) error {
	if action == "status" {
		if runtime.GOOS == "linux" {
			if e := trusted("/etc/systemd/system/raymond.service"); e != nil {
				return e
			}
			return native("systemctl", "status", "raymond.service")
		}
		if runtime.GOOS == "darwin" {
			if e := trusted("/Library/LaunchDaemons/dev.raymond.plist"); e != nil {
				return e
			}
			return native("launchctl", "print", "system/dev.raymond")
		}
		return fmt.Errorf("unsupported platform")
	}
	if e := root(); e != nil {
		return e
	}
	unlock, e := lifecycleLock()
	if e != nil {
		return e
	}
	defer unlock()
	if action != "status" {
		if e := root(); e != nil {
			return e
		}
	}
	if _, e := Installed(); e != nil {
		return e
	}
	if runtime.GOOS == "linux" {
		if action == "start" || action == "restart" {
			c, e := config.Load(ConfigPath)
			if e != nil {
				return e
			}
			if e = PlatformCheck(c.DNS.Zones...); e != nil {
				return e
			}
			if _, e = net.InterfaceByName("raymond0"); e != nil {
				if e = native("networkctl", "reload"); e != nil {
					return e
				}
			}
			if e = checkLinuxLink(c); e != nil {
				return e
			}
		}
		return native("systemctl", action, "raymond.service")
	}
	if runtime.GOOS != "darwin" {
		return fmt.Errorf("unsupported platform")
	}
	switch action {
	case "start":
		if e := ensureAliasesLocked(); e != nil {
			return e
		}
		if probeNative("launchctl", "print", "system/dev.raymond") == nil {
			return nil
		}
		return native("launchctl", "bootstrap", "system", "/Library/LaunchDaemons/dev.raymond.plist")
	case "stop":
		return native("launchctl", "bootout", "system/dev.raymond")
	case "restart":
		return native("launchctl", "kickstart", "-k", "system/dev.raymond")
	case "status":
		return native("launchctl", "print", "system/dev.raymond")
	}
	return fmt.Errorf("unknown lifecycle action")
}
func Uninstall() error {
	if e := root(); e != nil {
		return e
	}
	unlock, e := lifecycleLock()
	if e != nil {
		return e
	}
	defer unlock()
	s, e := ReadState(statePath)
	if os.IsNotExist(e) {
		return nil
	}
	if e != nil {
		return e
	}
	if e = trusted(statePath); e != nil {
		return e
	}
	if e = Verify(s); e != nil {
		return e
	}
	switch runtime.GOOS {
	case "linux":
		if probeNative("systemctl", "is-active", "--quiet", "raymond.service") == nil {
			if e = native("systemctl", "stop", "raymond.service"); e != nil {
				return e
			}
		}
		if _, err := os.Stat("/etc/systemd/system/raymond.service"); err == nil {
			if e = native("systemctl", "disable", "raymond.service"); e != nil {
				return e
			}
		}
	case "darwin":
		if probeNative("launchctl", "print", "system/dev.raymond") == nil {
			if e = native("launchctl", "bootout", "system/dev.raymond"); e != nil {
				return e
			}
		}
	}
	// Resolver/service files are removed only after their exact recorded digests match.
	for _, r := range s.Files {
		if e = verify(r); e != nil {
			return e
		}
	}
	if runtime.GOOS == "darwin" {
		assigned, e := aliases()
		if e != nil {
			return e
		}
		for _, a := range s.Aliases {
			if a != "127.0.0.42" && a != "127.0.0.43" {
				return fmt.Errorf("invalid alias journal")
			}
			if assigned[a] {
				if e = native("ifconfig", "lo0", "-alias", a); e != nil {
					return e
				}
			}
		}
	}
	if runtime.GOOS == "linux" {
		if _, err := net.InterfaceByName("raymond0"); err == nil {
			b, err := os.ReadFile("/sys/class/net/raymond0/ifalias")
			if err != nil || strings.TrimSpace(string(b)) != "Raymond private DNS routing" {
				return fmt.Errorf("cannot prove raymond0 ownership; refusing network cleanup")
			}
		}
	}
	if e = Remove("", s); e != nil {
		return e
	}
	if runtime.GOOS == "linux" {
		if e = native("systemctl", "daemon-reload"); e != nil {
			return e
		}
		if e = native("udevadm", "control", "--reload"); e != nil {
			return e
		}
		if e = native("networkctl", "reload"); e != nil {
			return e
		}
		if _, e = net.InterfaceByName("raymond0"); e == nil {
			if e = native("networkctl", "delete", "raymond0"); e != nil {
				return e
			}
		}
	}
	return os.Remove(statePath)
}

func trustedParent(path string) error {
	p := filepath.Dir(path)
	for {
		if _, e := os.Lstat(p); e == nil {
			return trusted(p)
		} else if !os.IsNotExist(e) {
			return e
		}
		p = filepath.Dir(p)
	}
}

func lifecycleLock() (func(), error) {
	if e := trustedParent(statePath); e != nil {
		return nil, e
	}
	if e := os.MkdirAll(filepath.Dir(statePath), 0700); e != nil {
		return nil, e
	}
	return lock(filepath.Join(filepath.Dir(statePath), "install.lock"))
}
func lock(path string) (func(), error) {
	if e := SafePath(path); e != nil {
		return nil, e
	}
	f, e := os.OpenFile(path, os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0600)
	if e != nil {
		return nil, e
	}
	if e = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); e != nil {
		f.Close()
		return nil, fmt.Errorf("another Raymond lifecycle operation is active: %w", e)
	}
	return func() { _ = f.Close() }, nil
}

// PlatformCheck checks existing resolver compatibility without changing it.
func PlatformCheck(zones ...string) error {
	if runtime.GOOS == "linux" {
		if e := linux.Check(native); e != nil {
			return e
		}
		p, e := filepath.EvalSymlinks("/etc/resolv.conf")
		if e != nil || p != "/run/systemd/resolve/stub-resolv.conf" {
			return fmt.Errorf("linux integration requires the existing systemd-resolved stub resolv.conf symlink; refusing to rewire the resolver")
		}
		if len(zones) > 0 {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			out, e := exec.CommandContext(ctx, "/usr/bin/resolvectl", "domain").Output()
			if e != nil {
				return fmt.Errorf("cannot inspect existing DNS scope ownership: %w", e)
			}
			if e = linux.CheckZones(zones, string(out)); e != nil {
				return e
			}
		}
	}
	return nil
}
func checkLinuxLink(c config.Config) error {
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, e := net.InterfaceByName("raymond0"); e == nil {
			break
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("networkd did not create raymond0")
		}
		time.Sleep(100 * time.Millisecond)
	}
	if e := native("networkctl", "reconfigure", "raymond0"); e != nil {
		return e
	}
	want := append([]string(nil), c.DNS.Zones...)
	for i := range want {
		want[i] = "~" + want[i]
	}
	sort.Strings(want)
	for {
		var last error
		for _, q := range []struct {
			kind string
			want []string
		}{{"dns", []string{"127.0.0.43"}}, {"domain", want}, {"default-route", []string{"no"}}} {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			b, e := exec.CommandContext(ctx, "/usr/bin/resolvectl", q.kind, "raymond0").Output()
			cancel()
			if e != nil {
				last = e
				break
			}
			parts := strings.SplitN(string(b), ":", 2)
			if len(parts) != 2 {
				last = fmt.Errorf("unexpected resolvectl output")
				break
			}
			got := strings.Fields(parts[1])
			sort.Strings(got)
			if strings.Join(got, " ") != strings.Join(q.want, " ") {
				last = fmt.Errorf("unproven route-only DNS ownership on raymond0 (%s)", q.kind)
				break
			}
		}
		if last == nil {
			return nil
		}
		if time.Now().After(deadline) {
			return last
		}
		time.Sleep(100 * time.Millisecond)
	}
}
