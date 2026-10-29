//go:build osintegration && (darwin || linux)

package acceptance

import (
	"context"
	"crypto/sha256"
	"debug/buildinfo"
	"debug/elf"
	"debug/macho"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/cbspire/raymond/internal/config"
	"gopkg.in/yaml.v3"
)

func command(name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}
func must(t *testing.T, name string, args ...string) []byte {
	t.Helper()
	out, e := command(name, args...)
	if e != nil {
		t.Fatalf("%s %v: %v\n%s", name, args, e, out)
	}
	return out
}
func requireDisposable(t *testing.T) {
	t.Helper()
	if os.Getenv("RAYMOND_PRIVILEGED_SMOKE") != "1" || os.Geteuid() != 0 {
		t.Fatal("requires explicitly reviewed disposable runner and root")
	}
}
func nativePath(name string) string {
	for _, dir := range []string{"/usr/bin", "/bin", "/usr/sbin", "/sbin"} {
		p := filepath.Join(dir, name)
		if _, e := os.Stat(p); e == nil {
			return p
		}
	}
	return name
}
func write(t *testing.T, path string, b []byte, mode os.FileMode) {
	t.Helper()
	if e := os.MkdirAll(filepath.Dir(path), 0755); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(path, b, mode); e != nil {
		t.Fatal(e)
	}
	if e := os.Chmod(path, mode); e != nil {
		t.Fatal(e)
	}
}
func aliases(t *testing.T) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	ifs, e := net.Interfaces()
	if e != nil {
		t.Fatal(e)
	}
	for _, i := range ifs {
		addrs, e := i.Addrs()
		if e != nil {
			t.Fatal(e)
		}
		for _, a := range addrs {
			ip, _, e := net.ParseCIDR(a.String())
			if e == nil {
				out[ip.String()] = true
			}
		}
	}
	return out
}
func eventually(t *testing.T, what string, check func() error) {
	t.Helper()
	deadline := time.Now().Add(45 * time.Second)
	var err error
	for {
		err = check()
		if err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s: %v", what, err)
		}
		time.Sleep(150 * time.Millisecond)
	}
}
func noRuntimeDependencies(t *testing.T, path string) {
	t.Helper()
	info, e := buildinfo.ReadFile(path)
	if e != nil {
		t.Fatal(e)
	}
	cgo := ""
	for _, s := range info.Settings {
		if s.Key == "CGO_ENABLED" {
			cgo = s.Value
		}
	}
	if cgo != "0" {
		t.Fatalf("distributed binary has CGO_ENABLED=%q", cgo)
	}
	if runtime.GOOS == "linux" {
		f, e := elf.Open(path)
		if e != nil {
			t.Fatal(e)
		}
		defer f.Close()
		for _, p := range f.Progs {
			if p.Type == elf.PT_INTERP {
				t.Fatal("Linux binary requires a dynamic loader")
			}
		}
	} else {
		f, e := macho.Open(path)
		if e != nil {
			t.Fatal(e)
		}
		defer f.Close()
		libs, e := f.ImportedLibraries()
		if e != nil {
			t.Fatal(e)
		}
		for _, lib := range libs {
			if !strings.HasPrefix(lib, "/usr/lib/") && !strings.HasPrefix(lib, "/System/Library/") {
				t.Fatalf("non-system runtime dependency %s", lib)
			}
		}
	}
	if _, e := exec.LookPath("go"); e == nil {
		t.Fatal("acceptance runtime PATH must exclude Go")
	}
}

type apps struct {
	mu          sync.Mutex
	connections map[string]bool
}

func (a *apps) serve(t *testing.T, port int) *http.Server {
	t.Helper()
	l, e := net.Listen("tcp4", fmt.Sprintf("127.0.0.1:%d", port))
	if e != nil {
		t.Fatal(e)
	}
	s := &http.Server{ReadHeaderTimeout: 5 * time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if port == 80 {
			fmt.Fprint(w, "localhost unchanged")
			return
		}
		a.mu.Lock()
		a.connections[r.RemoteAddr] = true
		a.mu.Unlock()
		if r.URL.Path == "/wp-login.php" {
			http.SetCookie(w, &http.Cookie{Name: "wordpress_logged_in", Value: "acceptance", Path: "/", HttpOnly: true})
			w.Header().Set("Location", "http://"+r.Host+"/wp-admin/")
			w.WriteHeader(302)
			return
		}
		fmt.Fprintf(w, "%d %s %s", port, r.Host, r.URL.RequestURI())
	})}
	go func() { _ = s.Serve(l) }()
	t.Cleanup(func() { _ = s.Close() })
	return s
}
func (a *apps) reset() { a.mu.Lock(); defer a.mu.Unlock(); a.connections = map[string]bool{} }
func client() *http.Client {
	return &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{Proxy: nil, DialContext: (&net.Dialer{Timeout: 3 * time.Second}).DialContext, MaxIdleConns: 100, MaxIdleConnsPerHost: 10}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}
func fetch(c *http.Client, host string, index int) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ips, e := net.DefaultResolver.LookupHost(ctx, host)
	if e != nil {
		return e
	}
	if len(ips) == 0 {
		return fmt.Errorf("no OS DNS results")
	}
	for _, ip := range ips {
		if ip != "127.0.0.42" {
			return fmt.Errorf("OS DNS escaped: %s -> %v", host, ips)
		}
	}
	res, e := c.Get("http://" + host + "/wp-admin/?original=1")
	if e != nil {
		return e
	}
	defer res.Body.Close()
	b, e := io.ReadAll(res.Body)
	want := fmt.Sprintf("%d %s /wp-admin/?original=1", 9001+index%2, host)
	if e != nil || res.StatusCode != 200 || string(b) != want {
		return fmt.Errorf("%s got %s %q; want %q (%v)", host, res.Status, b, want, e)
	}
	return nil
}
func pid() (int, error) {
	var b []byte
	var e error
	if runtime.GOOS == "linux" {
		b, e = command(nativePath("systemctl"), "show", "--property=MainPID", "--value", "raymond.service")
	} else {
		b, e = command(nativePath("launchctl"), "print", "system/dev.raymond")
		if e == nil {
			m := regexp.MustCompile(`(?m)^\s*pid = ([0-9]+)`).FindSubmatch(b)
			if len(m) != 2 {
				return 0, fmt.Errorf("no launchd PID")
			}
			b = m[1]
		}
	}
	if e != nil {
		return 0, e
	}
	n, e := strconv.Atoi(strings.TrimSpace(string(b)))
	if e != nil || n <= 1 || n == os.Getpid() {
		return 0, fmt.Errorf("invalid service PID %q", b)
	}
	return n, nil
}

func snapshot(t *testing.T) map[string][32]byte {
	t.Helper()
	out := map[string][32]byte{}
	for _, root := range []string{"/etc/resolver", "/etc/systemd/network", "/etc/systemd/resolved.conf.d", "/etc/systemd/system/raymond.service.d"} {
		e := filepath.WalkDir(root, func(p string, d os.DirEntry, e error) error {
			if os.IsNotExist(e) {
				return nil
			}
			if e != nil {
				return e
			}
			if d.IsDir() {
				return nil
			}
			st, e := os.Stat(p)
			if os.IsNotExist(e) {
				return nil
			}
			if e != nil {
				return e
			}
			if !st.Mode().IsRegular() {
				return nil
			}
			b, e := os.ReadFile(p)
			if e != nil {
				return e
			}
			out[p] = sha256.Sum256(b)
			return nil
		})
		if e != nil {
			t.Fatal(e)
		}
	}
	return out
}

func TestInstalledLifecycle(t *testing.T) {
	requireDisposable(t)
	if _, e := os.Lstat("/var/lib/raymond/state.json"); !os.IsNotExist(e) {
		t.Fatal("runner already has an installation; refusing to alter it")
	}
	src := candidate(t)
	if e := os.MkdirAll("/var/lib", 0755); e != nil {
		t.Fatal(e)
	}
	dir, e := os.MkdirTemp("/var/lib", "raymond-acceptance-")
	if e != nil {
		t.Fatal(e)
	}
	if e = os.Chmod(dir, 0755); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	b, e := os.ReadFile(src)
	if e != nil {
		t.Fatal(e)
	}
	binary := filepath.Join(dir, "raymond")
	write(t, binary, b, 0755)
	t.Cleanup(func() {
		out, e := command(binary, "uninstall")
		if e != nil {
			t.Errorf("cleanup uninstall: %v\n%s", e, out)
		}
	})
	configPath := filepath.Join(dir, "source.yaml")
	c := config.Config{Version: 1, DNS: config.DNS{Zones: []string{"test", "internal"}}}
	for i := 0; i < 10; i++ {
		zone := "test"
		if i >= 5 {
			zone = "internal"
		}
		c.Installations = append(c.Installations, config.Installation{Domain: fmt.Sprintf("install%d.web.%s", i, zone), Target: fmt.Sprintf("127.0.0.1:%d", 9001+i%2)})
	}
	if e = c.Validate(); e != nil {
		t.Fatal(e)
	}
	b, e = yaml.Marshal(c)
	if e != nil {
		t.Fatal(e)
	}
	write(t, configPath, b, 0644)
	initialAliases := aliases(t)
	sentinel := "/etc/resolver/unrelated.raymond.invalid"
	if runtime.GOOS == "linux" {
		sentinel = "/etc/systemd/system/raymond.service.d/90-acceptance-sentinel.conf"
	}
	if _, e = os.Lstat(sentinel); !os.IsNotExist(e) {
		t.Fatal("sentinel path exists")
	}
	sentinelData := []byte("# acceptance-owned unrelated resource\n")
	if runtime.GOOS == "linux" {
		sentinelData = []byte("[Service]\nEnvironment=RAYMOND_ACCEPTANCE_SENTINEL=preserve\n")
	}
	write(t, sentinel, sentinelData, 0644)
	t.Cleanup(func() { _ = os.Remove(sentinel) })
	before := snapshot(t)
	conflict := "/etc/resolver/test"
	if runtime.GOOS == "linux" {
		conflict = "/etc/systemd/network/80-raymond.network"
	}
	if _, e = os.Lstat(conflict); !os.IsNotExist(e) {
		t.Fatal("reserved resource path exists")
	}
	write(t, conflict, []byte("unrelated existing file\n"), 0644)
	if out, e := command(binary, "install", "--config", configPath); e == nil {
		t.Fatalf("installer overwrote conflict: %s", out)
	}
	got, e := os.ReadFile(conflict)
	if e != nil || string(got) != "unrelated existing file\n" {
		t.Fatal("conflicting file changed")
	}
	if e = os.Remove(conflict); e != nil {
		t.Fatal(e)
	}
	if _, e = os.Stat("/var/lib/raymond/state.json"); !os.IsNotExist(e) {
		t.Fatal("conflict left an installation journal")
	}
	a := &apps{connections: map[string]bool{}}
	localhost := a.serve(t, 80)
	a.serve(t, 9001)
	a.serve(t, 9002)
	must(t, binary, "install", "--config", configPath)
	must(t, binary, "install", "--config", configPath)
	must(t, binary, "doctor")
	// Prove a wildcard conflict is reported without terminating the listener.
	_ = localhost.Close()
	wild, e := net.Listen("tcp4", "0.0.0.0:80")
	if e != nil {
		t.Fatal(e)
	}
	if out, e := command(binary, "doctor"); e == nil {
		_ = wild.Close()
		t.Fatalf("doctor accepted wildcard listener: %s", out)
	}
	_ = wild.Close()
	a.serve(t, 80)
	must(t, binary, "start")
	httpClient := client()
	defer httpClient.CloseIdleConnections()
	eventually(t, "native DNS and HTTP", func() error { return fetch(httpClient, "first.install0.web.test", 0) })
	must(t, binary, "status")
	must(t, binary, "install", "--config", configPath)
	a.reset()
	for i := 0; i < 16; i++ {
		if e = fetch(httpClient, "first.install0.web.test", 0); e != nil {
			t.Fatal(e)
		}
	}
	a.mu.Lock()
	reused := len(a.connections)
	a.mu.Unlock()
	if reused > 2 {
		t.Fatalf("sequential backend connections not reused: %d", reused)
	}
	start := time.Now()
	var wg sync.WaitGroup
	for i, r := range c.Installations {
		for j := 0; j < 5; j++ {
			wg.Add(1)
			go func(i, j int, domain string) {
				defer wg.Done()
				if e := fetch(httpClient, fmt.Sprintf("deep.site%d.%s", j, domain), i); e != nil {
					t.Error(e)
				}
			}(i, j, r.Domain)
		}
	}
	wg.Wait()
	t.Logf("50 native-OS-resolved concurrent requests: %s", time.Since(start))
	response, e := httpClient.Get("http://first.install0.web.test/wp-login.php")
	if e != nil {
		t.Fatal(e)
	}
	response.Body.Close()
	if response.StatusCode != 302 || response.Header.Get("Location") != "http://first.install0.web.test/wp-admin/" || len(response.Cookies()) != 1 || response.Cookies()[0].Name != "wordpress_logged_in" {
		t.Fatal("WordPress-style redirect/cookie changed")
	}
	res, e := httpClient.Get("http://127.0.0.1/")
	if e != nil {
		t.Fatal(e)
	}
	data, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if string(data) != "localhost unchanged" {
		t.Fatal("localhost:80 affected")
	}
	old, e := pid()
	if e != nil {
		t.Fatal(e)
	}
	assertLoopbackListeners(t, old)
	rss := must(t, nativePath("ps"), "-o", "rss=", "-p", strconv.Itoa(old))
	t.Logf("daemon RSS KiB: %s", strings.TrimSpace(string(rss)))
	if runtime.GOOS == "linux" {
		user := strings.TrimSpace(string(must(t, nativePath("ps"), "-o", "user=", "-p", strconv.Itoa(old))))
		if user == "root" {
			t.Fatal("Linux service unexpectedly runs as root")
		}
		caps := strings.TrimSpace(string(must(t, nativePath("systemctl"), "show", "--property=CapabilityBoundingSet", "--value", "raymond.service")))
		if caps != "cap_net_bind_service" {
			t.Fatalf("excess Linux service capabilities: %s", caps)
		}
	}
	// Simulate an unexpected crash; the actual service manager must recover it.
	if e = syscall.Kill(old, syscall.SIGKILL); e != nil {
		t.Fatal(e)
	}
	eventually(t, "service-manager crash recovery", func() error {
		n, e := pid()
		if e != nil {
			return e
		}
		if n == old {
			return fmt.Errorf("old PID still present")
		}
		return fetch(httpClient, "after-crash.install0.web.test", 0)
	})
	must(t, binary, "restart")
	eventually(t, "controlled restart", func() error { return fetch(httpClient, "after-restart.install0.web.test", 0) })
	// Modified owned files must stop uninstall before touching the running service.
	original, e := os.ReadFile("/etc/raymond/config.yaml")
	if e != nil {
		t.Fatal(e)
	}
	func() {
		defer write(t, "/etc/raymond/config.yaml", original, 0644)
		write(t, "/etc/raymond/config.yaml", append(append([]byte(nil), original...), []byte("# operator edit\n")...), 0644)
		if out, e := command(binary, "uninstall"); e == nil {
			t.Fatalf("uninstall accepted modified config: %s", out)
		}
		if e = fetch(httpClient, "after-refusal.install0.web.test", 0); e != nil {
			t.Fatal(e)
		}
	}()
	must(t, binary, "stop")
	if runtime.GOOS == "darwin" {
		for _, ip := range []string{"127.0.0.42", "127.0.0.43"} {
			if !initialAliases[ip] {
				must(t, nativePath("ifconfig"), "lo0", "-alias", ip)
			}
		}
	} else {
		must(t, nativePath("networkctl"), "delete", "raymond0")
	}
	// Reconstruct the volatile interface state lost at boot, then start again.
	must(t, binary, "start")
	eventually(t, "volatile-state recovery", func() error { return fetch(httpClient, "after-cold-start.install0.web.test", 0) })
	must(t, binary, "uninstall")
	must(t, binary, "uninstall")
	for p, digest := range before {
		b, e := os.ReadFile(p)
		if e != nil || sha256.Sum256(b) != digest {
			t.Errorf("unrelated resource changed: %s (%v)", p, e)
		}
	}
	for _, path := range []string{"/etc/raymond/config.yaml", "/var/lib/raymond/state.json"} {
		if _, e = os.Lstat(path); !os.IsNotExist(e) {
			t.Errorf("owned resource remains: %s", path)
		}
	}
	if runtime.GOOS == "darwin" {
		for _, path := range []string{"/etc/resolver/test", "/etc/resolver/internal", "/Library/LaunchDaemons/dev.raymond.plist", "/Library/PrivilegedHelperTools/dev.raymond"} {
			if _, e = os.Lstat(path); !os.IsNotExist(e) {
				t.Error("owned resource remains:", path)
			}
		}
		after := aliases(t)
		for _, ip := range []string{"127.0.0.42", "127.0.0.43"} {
			if after[ip] != initialAliases[ip] {
				t.Error("loopback alias not restored:", ip)
			}
		}
	} else {
		for _, path := range []string{"/etc/systemd/network/80-raymond.netdev", "/etc/systemd/network/80-raymond.network", "/etc/systemd/system/raymond.service", "/usr/libexec/raymond/raymond"} {
			if _, e = os.Lstat(path); !os.IsNotExist(e) {
				t.Error("owned resource remains:", path)
			}
		}
		if _, e = net.InterfaceByName("raymond0"); e == nil {
			t.Error("owned network link remains")
		}
	}
	// On macOS the removed aliases make binds unavailable; check ports on Linux.
	if runtime.GOOS == "linux" {
		for _, addr := range []string{"127.0.0.42:80", "127.0.0.43:53"} {
			l, e := net.Listen("tcp4", addr)
			if e != nil {
				t.Error(e)
			} else {
				_ = l.Close()
			}
		}
	}
}
