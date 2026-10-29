//go:build osintegration && (darwin || linux)

package acceptance

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	mdns "github.com/miekg/dns"
)

func TestOfflineRuntime(t *testing.T) {
	requireDisposable(t)
	if runtime.GOOS == "linux" && os.Getenv("RAYMOND_OFFLINE_NAMESPACE") != "1" {
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, nativePath("unshare"), "--net", os.Args[0], "-test.run=^TestOfflineRuntime$", "-test.v", "-test.timeout=75s")
		cmd.Env = append(os.Environ(), "RAYMOND_OFFLINE_NAMESPACE=1")
		out, e := cmd.CombinedOutput()
		t.Logf("isolated network namespace:\n%s", out)
		if e != nil {
			t.Fatal(e)
		}
		return
	}
	binary := candidate(t)
	if runtime.GOOS == "linux" {
		must(t, nativePath("ip"), "link", "set", "lo", "up")
		if out := must(t, nativePath("ip"), "route", "show", "default"); strings.TrimSpace(string(out)) != "" {
			t.Fatal("offline namespace has an external route")
		}
	}
	// A namespace has its own loopback. On macOS the lifecycle test restored aliases,
	// so create and restore only the missing aliases needed by this foreground test.
	if runtime.GOOS == "darwin" {
		before := aliases(t)
		for _, ip := range []string{"127.0.0.42", "127.0.0.43"} {
			if !before[ip] {
				must(t, nativePath("ifconfig"), "lo0", "alias", ip, "up")
				t.Cleanup(func() { must(t, nativePath("ifconfig"), "lo0", "-alias", ip) })
			}
		}
	}
	apps := &apps{connections: map[string]bool{}}
	apps.serve(t, 9001)
	configPath := filepath.Join(t.TempDir(), "offline.yaml")
	write(t, configPath, []byte("version: 1\ndns:\n  zones: [test]\ninstallations:\n  - domain: install.test\n    target: 127.0.0.1:9001\n"), 0600)
	args := []string{"serve", "--config", configPath}
	program := binary
	if runtime.GOOS == "darwin" {
		// Kernel-enforced outbound policy; only localhost backends and DNS replies work.
		policy := `(version 1)(allow default)(deny network-outbound)(allow network-outbound (remote ip "localhost:*"))`
		program = nativePath("sandbox-exec")
		args = append([]string{"-p", policy, binary}, args...)
	}
	cmd := exec.Command(program, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if e := cmd.Start(); e != nil {
		t.Fatal(e)
	}
	stopped := make(chan error, 1)
	go func() { stopped <- cmd.Wait() }()
	t.Cleanup(func() {
		_ = cmd.Process.Signal(syscall.SIGTERM)
		select {
		case e := <-stopped:
			if e != nil {
				t.Errorf("offline daemon shutdown: %v", e)
			}
		case <-time.After(12 * time.Second):
			_ = cmd.Process.Kill()
			t.Error("offline daemon did not shut down")
		}
	})
	c := client()
	defer c.CloseIdleConnections()
	eventually(t, "offline DNS and HTTP", func() error {
		q := new(mdns.Msg)
		q.SetQuestion("deep.site.install.test.", mdns.TypeA)
		answer, _, e := (&mdns.Client{Timeout: time.Second}).Exchange(q, "127.0.0.43:53")
		if e != nil {
			return e
		}
		if answer.Rcode != 0 || !answer.Authoritative || len(answer.Answer) != 1 || answer.Answer[0].(*mdns.A).A.String() != "127.0.0.42" {
			return fmt.Errorf("incorrect authoritative offline answer: %v", answer)
		}
		req, e := http.NewRequest("GET", "http://127.0.0.42/wp-admin/?original=1", nil)
		if e != nil {
			return e
		}
		req.Host = "deep.site.install.test"
		res, e := c.Do(req)
		if e != nil {
			return e
		}
		defer res.Body.Close()
		if res.StatusCode != 200 {
			return fmt.Errorf("offline proxy status %s", res.Status)
		}
		return nil
	})
	t.Log("DNS and HTTP succeeded with kernel-restricted external networking and no Go runtime on PATH")
}
