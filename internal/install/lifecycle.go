package install

import (
	"bytes"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"

	"github.com/cbspire/raymond/internal/config"
	"github.com/cbspire/raymond/internal/diagnostics"
	darwin "github.com/cbspire/raymond/internal/platform/darwin"
	linux "github.com/cbspire/raymond/internal/platform/linux"
	"gopkg.in/yaml.v3"
)

const ConfigPath = "/etc/raymond/config.yaml"
const statePath = "/var/lib/raymond/state.json"
const binaryPath = "/usr/local/libexec/raymond/raymond"

func root() error {
	if os.Geteuid() != 0 {
		return fmt.Errorf("administrator privileges required; explicitly rerun with sudo")
	}
	return nil
}
func native(name string, args ...string) error {
	paths := map[string]string{"systemctl": "/usr/bin/systemctl", "resolvectl": "/usr/bin/resolvectl", "networkctl": "/usr/bin/networkctl", "ip": "/usr/sbin/ip", "launchctl": "/bin/launchctl", "ifconfig": "/sbin/ifconfig"}
	path, ok := paths[name]
	if !ok {
		return fmt.Errorf("unsupported native command")
	}
	c := exec.Command(path, args...)
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	return c.Run()
}
func trusted(path string) error {
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
		if !ok || s.Uid != 0 {
			return fmt.Errorf("%s must be root-owned", p)
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
func aliases() map[string]bool {
	found := map[string]bool{}
	is, _ := net.Interfaces()
	for _, i := range is {
		as, _ := i.Addrs()
		for _, a := range as {
			ip, _, e := net.ParseCIDR(a.String())
			if e == nil {
				found[ip.String()] = true
			}
		}
	}
	return found
}
func Install(path string) error {
	if e := root(); e != nil {
		return e
	}
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
	if e = trusted(exe); e != nil {
		return fmt.Errorf("install a reviewed root-owned executable first: %w", e)
	}
	b, e = os.ReadFile(exe)
	if e != nil {
		return e
	}
	files[binaryPath] = b
	for p := range files {
		if e = trustedParent(p); e != nil {
			return e
		}
	}
	if e = trustedParent(statePath); e != nil {
		return e
	}
	if old, e := ReadState(statePath); e == nil {
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
	var created []string
	if runtime.GOOS == "linux" {
		if e = linux.Check(native); e != nil {
			return e
		}
		if _, e = net.InterfaceByName("raymond0"); e == nil {
			return fmt.Errorf("raymond0 already exists; refusing ambiguous ownership")
		}
	} else {
		for _, a := range []string{"127.0.0.42", "127.0.0.43"} {
			if !aliases()[a] {
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
		if e = EnsureAliases(); e != nil {
			return fail(e)
		}
		if e = diagnostics.Check(c); e != nil {
			return fail(e)
		}
	} else {
		if e = native("systemctl", "daemon-reload"); e != nil {
			return fail(e)
		}
		if e = native("networkctl", "reload"); e != nil {
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
	s, e := Installed()
	if e != nil {
		return e
	}
	for _, a := range s.Aliases {
		if a != "127.0.0.42" && a != "127.0.0.43" {
			return fmt.Errorf("invalid alias journal")
		}
		if !aliases()[a] {
			if e = native("ifconfig", "lo0", "alias", a, "up"); e != nil {
				return e
			}
		}
	}
	return nil
}
func Control(action string) error {
	if action != "status" {
		if e := root(); e != nil {
			return e
		}
	}
	if _, e := Installed(); e != nil {
		return e
	}
	if runtime.GOOS == "linux" {
		return native("systemctl", action, "raymond.service")
	}
	if runtime.GOOS != "darwin" {
		return fmt.Errorf("unsupported platform")
	}
	switch action {
	case "start":
		if e := EnsureAliases(); e != nil {
			return e
		}
		if native("launchctl", "print", "system/dev.raymond") == nil {
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
	if runtime.GOOS == "linux" {
		if native("systemctl", "is-active", "--quiet", "raymond.service") == nil {
			if e = native("systemctl", "stop", "raymond.service"); e != nil {
				return e
			}
		}
		if e = native("systemctl", "disable", "raymond.service"); e != nil {
			return e
		}
	} else if runtime.GOOS == "darwin" {
		if native("launchctl", "print", "system/dev.raymond") == nil {
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
		for _, a := range s.Aliases {
			if a != "127.0.0.42" && a != "127.0.0.43" {
				return fmt.Errorf("invalid alias journal")
			}
			if aliases()[a] {
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
