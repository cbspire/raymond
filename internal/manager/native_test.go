//go:build osintegration && (darwin || linux)

package manager

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/cbspire/raymond/internal/config"
	"github.com/cbspire/raymond/internal/control"
	"github.com/cbspire/raymond/internal/install"
	"github.com/cbspire/raymond/internal/release"
)

type nativeTransport func(*http.Request) (*http.Response, error)

func (f nativeTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func nativeCommand(t *testing.T, name string, args ...string) []byte {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = append(os.Environ(), "SUDO_UID=", "SUDO_GID=")
	b, e := cmd.CombinedOutput()
	if e != nil {
		t.Fatalf("native command %s failed: %v\n%s", name, e, b)
	}
	return b
}
func ownerCommand(t *testing.T, owner, binary string, args ...string) []byte {
	t.Helper()
	return nativeCommand(t, "/usr/bin/sudo", append([]string{"-n", "-H", "-u", owner, "--", binary}, args...)...)
}
func nativeEventually(t *testing.T, description string, check func() error) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for {
		e := check()
		if e == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s: %v", description, e)
		}
		time.Sleep(100 * time.Millisecond)
	}
}
func testOwnerFile(t *testing.T, path string, data []byte, uid, gid int, mode os.FileMode) {
	t.Helper()
	if e := os.WriteFile(path, data, mode); e != nil {
		t.Fatal(e)
	}
	if e := os.Chown(path, uid, gid); e != nil {
		t.Fatal(e)
	}
}
func nativeResolverFixture(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "linux" {
		return
	}
	target, e := filepath.EvalSymlinks("/etc/resolv.conf")
	if e == nil && target == "/run/systemd/resolve/stub-resolv.conf" {
		return
	}
	st, e := os.Lstat("/etc/resolv.conf")
	if e != nil {
		t.Fatal(e)
	}
	var old []byte
	link := ""
	if st.Mode()&os.ModeSymlink != 0 {
		link, e = os.Readlink("/etc/resolv.conf")
	} else {
		old, e = os.ReadFile("/etc/resolv.conf")
	}
	if e != nil {
		t.Fatal(e)
	}
	dir, e := os.MkdirTemp("/etc", "raymond-manager-resolver-")
	if e != nil {
		t.Fatal(e)
	}
	temp := filepath.Join(dir, "resolv.conf")
	if e = os.Symlink("/run/systemd/resolve/stub-resolv.conf", temp); e != nil {
		t.Fatal(e)
	}
	if e = os.Rename(temp, "/etc/resolv.conf"); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		defer os.RemoveAll(dir)
		var e error
		if link != "" {
			e = os.Symlink(link, temp)
		} else {
			e = os.WriteFile(temp, old, st.Mode().Perm())
		}
		if e == nil {
			e = os.Rename(temp, "/etc/resolv.conf")
		}
		if e != nil {
			t.Error("restore resolver fixture", e)
		}
	})
}
func nativeRelease(t *testing.T, version, binary string) *release.Client {
	t.Helper()
	payload, e := os.ReadFile(binary)
	if e != nil {
		t.Fatal(e)
	}
	var archive bytes.Buffer
	gzipWriter := gzip.NewWriter(&archive)
	tarWriter := tar.NewWriter(gzipWriter)
	if e = tarWriter.WriteHeader(&tar.Header{Name: "raymond", Mode: 0755, Size: int64(len(payload)), Typeflag: tar.TypeReg}); e != nil {
		t.Fatal(e)
	}
	if _, e = tarWriter.Write(payload); e != nil {
		t.Fatal(e)
	}
	tarWriter.Close()
	gzipWriter.Close()
	sum := sha256.Sum256(archive.Bytes())
	name := fmt.Sprintf("raymond_%s_%s_%s.tar.gz", version, runtime.GOOS, runtime.GOARCH)
	client := release.NewClient()
	client.HTTP.Transport = nativeTransport(func(r *http.Request) (*http.Response, error) {
		var data []byte
		switch r.URL.String() {
		case release.Repo + "/releases/download/" + version + "/checksums.txt":
			data = []byte(fmt.Sprintf("%x  %s\n", sum, name))
		case release.Repo + "/releases/download/" + version + "/" + name:
			data = archive.Bytes()
		default:
			return nil, fmt.Errorf("unexpected release request")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(data)), Request: r}, nil
	})
	return client
}
func nativeWaitOperation(t *testing.T, client *control.Client, id string) control.Operation {
	t.Helper()
	var op control.Operation
	nativeEventually(t, "operation completion", func() error {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if e := client.Call(ctx, "GET", control.Prefix+"operations/"+id, nil, &op); e != nil {
			return e
		}
		if op.Status == "queued" || op.Status == "running" {
			return fmt.Errorf("still active")
		}
		return nil
	})
	return op
}
func TestNativeManagedLifecycle(t *testing.T) {
	if os.Getenv("RAYMOND_PRIVILEGED_SMOKE") != "1" || os.Geteuid() != 0 {
		t.Fatal("native management acceptance requires a reviewed disposable supported runner")
	}
	if _, e := os.Stat("/var/lib/raymond/state.json"); !os.IsNotExist(e) {
		t.Fatal("refusing existing installation")
	}
	owner := os.Getenv("RAYMOND_TEST_USER")
	u, e := user.Lookup(owner)
	if e != nil {
		t.Fatal(e)
	}
	uid, _ := strconv.Atoi(u.Uid)
	gid, _ := strconv.Atoi(u.Gid)
	if uid <= 0 {
		t.Fatal("ordinary owner required")
	}
	credentialDir := filepath.Join(u.HomeDir, ".raymond")
	if _, e = os.Lstat(credentialDir); !os.IsNotExist(e) {
		t.Fatal("refusing existing owner credential directory")
	}
	t.Cleanup(func() { _ = os.RemoveAll(credentialDir) })
	work, e := os.MkdirTemp(u.HomeDir, "raymond-manager-acceptance-")
	if e != nil {
		t.Fatal(e)
	}
	if e = os.Chown(work, uid, gid); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _ = os.RemoveAll(work) })
	nativeResolverFixture(t)
	backend := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "native backend") }))
	backend.Listener, e = net.Listen("tcp4", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	backend.Start()
	defer backend.Close()
	cfgPath := filepath.Join(work, "config.yaml")
	data := []byte("# Preserve owner comments\nversion: 1\ndns:\n  zones: [test]\nroutes:\n  - domain: studio.test\n    target: " + strings.TrimPrefix(backend.URL, "http://") + "\n")
	testOwnerFile(t, cfgPath, data, uid, gid, 0600)
	candidate := os.Getenv("RAYMOND_MANAGER_BINARY")
	binary, e := os.ReadFile(candidate)
	if e != nil {
		t.Fatal(e)
	}
	cli := filepath.Join(work, "raymond")
	testOwnerFile(t, cli, binary, uid, gid, 0755)
	t.Cleanup(func() {
		if _, e := os.Stat("/var/lib/raymond/state.json"); e == nil || install.ManagementAvailable() {
			ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, cli, "uninstall")
			cmd.Env = append(os.Environ(), "SUDO_UID=", "SUDO_GID=")
			if b, e := cmd.CombinedOutput(); e != nil {
				t.Errorf("native cleanup: %v %s", e, b)
			}
		}
	})
	// Migration is from the actual immutable 0.1.1 artifact, not just a cross-build.
	download, e := os.MkdirTemp("", "raymond-old-release-")
	if e != nil {
		t.Fatal(e)
	}
	defer os.RemoveAll(download)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	old, e := release.NewClient().Download(ctx, "0.1.1", runtime.GOOS, runtime.GOARCH, download)
	if e != nil {
		t.Fatal("download immutable migration baseline", e)
	}
	oldBytes, e := os.ReadFile(old)
	if e != nil {
		t.Fatal(e)
	}
	oldCLI := filepath.Join(work, "raymond-old")
	testOwnerFile(t, oldCLI, oldBytes, uid, gid, 0755)
	ownerCommand(t, owner, "/usr/bin/sudo", "-n", "--", oldCLI, "install", "--config", cfgPath)
	nativeCommand(t, oldCLI, "start")
	ownerCommand(t, owner, cli, "setup", "--config", cfgPath)
	metadata, e := install.ManagementMetadata()
	if e != nil {
		t.Fatal(e)
	}
	if metadata.UID != uid || metadata.ConfigPath != cfgPath || !install.ManagementReady() {
		t.Fatal("invalid enrollment")
	}
	tokenData, e := os.ReadFile(filepath.Join(credentialDir, "control.json"))
	if e != nil {
		t.Fatal(e)
	}
	var access struct {
		Token string `json:"token"`
	}
	if e = json.Unmarshal(tokenData, &access); e != nil || access.Token != metadata.OwnerToken {
		t.Fatal("owner credential delivery failed")
	}
	socketClient := control.NewClient(control.SocketPath, metadata.OwnerToken)
	defer socketClient.HTTP.CloseIdleConnections()
	state := func() control.State {
		t.Helper()
		var s control.State
		if e := socketClient.Call(ctx, "GET", control.Prefix+"state", nil, &s); e != nil {
			t.Fatal(e)
		}
		return s
	}
	original := state()
	if !original.Running || original.Pending {
		t.Fatal("migration did not preserve working config")
	}
	denyOwnerElevation(t, owner)
	ownerCommand(t, owner, cli, "status")
	ownerCommand(t, owner, cli, "doctor")
	ownerCommand(t, owner, cli, "setup")
	ownerCommand(t, owner, cli, "setup", "--config", cfgPath)
	// Browser launch/session exchange travels through the restricted real proxy.
	var launch struct {
		Token string `json:"token"`
	}
	if e = socketClient.Call(ctx, "POST", control.Prefix+"launch", nil, &launch); e != nil {
		t.Fatal(e)
	}
	browser := &http.Client{Transport: &http.Transport{Proxy: nil}, Timeout: 10 * time.Second}
	defer browser.CloseIdleConnections()
	req, _ := http.NewRequest("POST", "http://127.0.0.42/api/v1/session", strings.NewReader(`{"token":"`+launch.Token+`"}`))
	req.Host = "raymond.test"
	req.Header.Set("Origin", control.Origin)
	req.Header.Set("Content-Type", "application/json")
	response, e := browser.Do(req)
	if e != nil {
		t.Fatal(e)
	}
	var session struct {
		CSRF string `json:"csrf"`
	}
	if e = json.NewDecoder(response.Body).Decode(&session); e != nil {
		t.Fatal(e)
	}
	response.Body.Close()
	if response.StatusCode != 200 || len(response.Cookies()) != 1 {
		t.Fatal("restricted proxy credential delivery/session failed")
	}
	cookie := response.Cookies()[0]
	changed := original.Saved.Config
	changed.Routes = append([]config.Route(nil), changed.Routes...)
	changed.Routes = append(changed.Routes, config.Route{Domain: "*.apps.test", Target: strings.TrimPrefix(backend.URL, "http://")})
	var op control.Operation
	if e = socketClient.Call(ctx, "POST", control.Prefix+"operations", control.Request{Action: "save", Revision: original.Saved.Revision, Config: &changed}, &op); e != nil {
		t.Fatal(e)
	}
	if done := nativeWaitOperation(t, socketClient, op.ID); done.Status != "succeeded" {
		t.Fatal(done.Error)
	}
	saved, e := os.ReadFile(cfgPath)
	if e != nil || !bytes.Contains(saved, []byte("Preserve owner comments")) {
		t.Fatal("comments lost")
	}
	wildcardResponse, e := browser.Get("http://deep.eu.apps.test/")
	if e != nil {
		t.Fatal("native wildcard DNS/HTTP", e)
	}
	wildcardResponse.Body.Close()
	if wildcardResponse.StatusCode != 200 {
		t.Fatal("native wildcard routing failed")
	}
	// Invalid OS listeners are saved but activation restores the working snapshot.
	current := state()
	invalid := current.Saved.Config
	invalid.Proxy.Listen = "127.0.0.44:8080"
	if e = socketClient.Call(ctx, "POST", control.Prefix+"operations", control.Request{Action: "save", Revision: current.Saved.Revision, Config: &invalid}, &op); e != nil {
		t.Fatal(e)
	}
	if done := nativeWaitOperation(t, socketClient, op.ID); done.Status != "failed" {
		t.Fatal("activation unexpectedly succeeded")
	}
	if !state().Pending {
		t.Fatal("failed activation lost pending edits")
	}
	if e = socketClient.Call(ctx, "POST", control.Prefix+"operations", control.Request{Action: "discard"}, &op); e != nil {
		t.Fatal(e)
	}
	if done := nativeWaitOperation(t, socketClient, op.ID); done.Status != "succeeded" || state().Pending {
		t.Fatal("discard failed")
	}
	// A full native binary/CLI/manager replacement uses the real verified extractor
	// with a test transport for the fixed release URLs, never a configurable root URL.
	runUpdate := func(version, path string, failCLI bool) {
		t.Helper()
		j, e := control.OpenOperations(control.RootDir)
		if e != nil {
			t.Fatal(e)
		}
		defer j.Close()
		unlock, e := install.ManagedLock()
		if e != nil {
			t.Fatal(e)
		}
		defer unlock()
		b := NewBackend(metadata, state().Version)
		b.releases = nativeRelease(t, version, path)
		if failCLI {
			realOwner := b.Owner
			failed := false
			b.Owner = func(ctx context.Context, r OwnerRequest) (OwnerResult, error) {
				if r.Action == "cli-save" && !failed {
					result, e := realOwner(ctx, r)
					if e == nil {
						failed = true
						return result, fmt.Errorf("injected loss of publication acknowledgement")
					}
					return result, e
				}
				return realOwner(ctx, r)
			}
		}
		operation, e := j.Enqueue(control.Request{Action: "update", Version: version})
		if e != nil {
			t.Fatal(e)
		}
		if e = executeWorker(ctx, b, j, operation); e != nil {
			t.Fatal(e)
		}
		result, e := j.Get(operation.ID)
		if e != nil {
			t.Fatal(e)
		}
		expected := "succeeded"
		if failCLI {
			expected = "failed"
		}
		if result.Status != expected {
			t.Fatalf("native update status %s: %s", result.Status, result.Error)
		}
	}
	runUpdate("0.1.2", os.Getenv("RAYMOND_UPDATE_BINARY"), false)
	if state().Version != "0.1.2" {
		t.Fatal("manager version not replaced")
	}
	if out := ownerCommand(t, owner, cli, "version"); !strings.Contains(string(out), "raymond 0.1.2 (") {
		t.Fatal("owner CLI was not updated")
	}
	runUpdate("0.1.3", os.Getenv("RAYMOND_NEXT_BINARY"), true)
	if state().Version != "0.1.2" {
		t.Fatal("failed upgrade did not restore manager")
	}
	if out := ownerCommand(t, owner, cli, "version"); !strings.Contains(string(out), "raymond 0.1.2 (") {
		t.Fatal("failed upgrade did not restore owner CLI")
	}
	// Simulate a dead worker after protected resources changed: release its locks
	// with the running journal untouched. The actual manager must launch recovery.
	j, e := control.OpenOperations(control.RootDir)
	if e != nil {
		t.Fatal(e)
	}
	unlock, e := install.ManagedLock()
	if e != nil {
		t.Fatal(e)
	}
	interrupted, e := j.Enqueue(control.Request{Action: "update", Version: "0.1.3"})
	if e != nil {
		t.Fatal(e)
	}
	if e = j.Start(interrupted.ID); e != nil {
		t.Fatal(e)
	}
	stage, e := os.MkdirTemp(control.RootDir, "native-interruption-")
	if e != nil {
		t.Fatal(e)
	}
	defer os.RemoveAll(stage)
	next, e := nativeRelease(t, "0.1.3", os.Getenv("RAYMOND_NEXT_BINARY")).Download(ctx, "0.1.3", runtime.GOOS, runtime.GOARCH, stage)
	if e != nil {
		t.Fatal(e)
	}
	oldCLIData, e := os.ReadFile(cli)
	if e != nil {
		t.Fatal(e)
	}
	before := state()
	transaction, e := install.PrepareManaged(interrupted.ID, before.Active.Revision, next, oldCLIData)
	if e != nil {
		t.Fatal(e)
	}
	protectedConfig := filepath.Join(stage, "config.yaml")
	currentConfig, e := os.ReadFile(install.ConfigPath)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(protectedConfig, currentConfig, 0600); e != nil {
		t.Fatal(e)
	}
	if e = install.ApplyManaged(transaction, protectedConfig, next); e != nil {
		t.Fatal(e)
	}
	j.Close()
	unlock()
	if recovered := nativeWaitOperation(t, socketClient, interrupted.ID); recovered.Status != "failed" {
		t.Fatal("interrupted upgrade was not reconciled")
	}
	if state().Version != "0.1.2" {
		t.Fatal("orphan recovery did not restore manager")
	}
	// Browser sessions survive replacement and rollback, while unauthenticated and
	// cross-origin mutations remain rejected by the real installed proxy.
	request, _ := http.NewRequest("GET", "http://127.0.0.42/api/v1/state", nil)
	request.Host = "raymond.test"
	request.AddCookie(cookie)
	response, e = browser.Do(request)
	if e != nil {
		t.Fatal(e)
	}
	response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatal("session lost after native update")
	}
	unsafe, _ := http.NewRequest("POST", "http://127.0.0.42/api/v1/operations", strings.NewReader(`{"action":"stop"}`))
	unsafe.Host = "raymond.test"
	unsafe.Header.Set("Origin", "http://hostile.test")
	unsafe.Header.Set("Content-Type", "application/json")
	unsafe.AddCookie(cookie)
	response, e = browser.Do(unsafe)
	if e != nil {
		t.Fatal(e)
	}
	response.Body.Close()
	if response.StatusCode != 403 {
		t.Fatal("hostile origin accepted")
	}
	for _, body := range []string{`{"action":"restart","path":"/etc/passwd"}`, `{"action":"update","version":"https://hostile.invalid/a"}`} {
		request, _ := http.NewRequest("POST", "http://127.0.0.42/api/v1/operations", strings.NewReader(body))
		request.Host = "raymond.test"
		request.Header.Set("Origin", control.Origin)
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("X-Raymond-CSRF", session.CSRF)
		request.AddCookie(cookie)
		response, e = browser.Do(request)
		if e != nil {
			t.Fatal(e)
		}
		response.Body.Close()
		if response.StatusCode != 400 {
			t.Fatal("unsafe operation fields accepted")
		}
	}
	ownerCommand(t, owner, cli, "stop")
	if state().Running {
		t.Fatal("stop failed")
	}
	ownerCommand(t, owner, cli, "start")
	if !state().Running {
		t.Fatal("passwordless start failed")
	}
	ownerCommand(t, owner, cli, "restart")
	// Read live request logs through the root broker, not journal access granted to
	// the owner or proxy DynamicUser. No query string or payload may be projected.
	request, _ = http.NewRequest("GET", "http://127.0.0.42/hello?secret=native-private", nil)
	request.Host = "studio.test"
	response, e = browser.Do(request)
	if e != nil {
		t.Fatal(e)
	}
	io.Copy(io.Discard, response.Body)
	response.Body.Close()
	time.Sleep(300 * time.Millisecond)
	output := ownerCommand(t, owner, cli, "logs", "--no-follow", "--lines", "100")
	if !strings.Contains(string(output), `"host":"studio.test"`) || strings.Contains(string(output), "native-private") {
		t.Fatal("native log projection failed")
	}
	nativeCommand(t, cli, "uninstall")
	if _, e = os.Stat(filepath.Join(credentialDir, "control.json")); !os.IsNotExist(e) {
		t.Fatal("owner credential was not cleaned")
	}
	if _, e = os.Stat(control.RootDir); !os.IsNotExist(e) {
		t.Fatal("manager resources remain")
	}
	if _, e = os.Stat(cfgPath); e != nil {
		t.Fatal("uninstall removed owner config")
	}
}

// Deny subsequent sudo use by the enrolled account on this disposable runner.
// The root test driver retains authority and restores the exact original policy.
func denyOwnerElevation(t *testing.T, owner string) {
	t.Helper()
	if strings.ContainsAny(owner, "\r\n\\%+=,: ") {
		t.Fatal("unsupported fixture owner")
	}
	const policy = "/etc/sudoers"
	original, e := os.ReadFile(policy)
	if e != nil {
		t.Fatal(e)
	}
	st, e := os.Stat(policy)
	if e != nil {
		t.Fatal(e)
	}
	fixture := append(append([]byte(nil), original...), []byte("\n# Raymond disposable acceptance: owner management must not elevate\n"+owner+" ALL=(ALL:ALL) !ALL\n")...)
	if e = control.AtomicWrite(policy, fixture, st.Mode().Perm()); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		current, e := os.ReadFile(policy)
		if e != nil || !bytes.Equal(current, fixture) {
			t.Error("sudoers fixture changed unexpectedly; refusing overwrite")
			return
		}
		if e = control.AtomicWrite(policy, original, st.Mode().Perm()); e != nil {
			t.Error("restore sudoers fixture", e)
		}
	})
	nativeCommand(t, "/usr/sbin/visudo", "-c", "-f", policy)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "/usr/bin/sudo", "-n", "-H", "-u", owner, "--", "/usr/bin/sudo", "-n", "true")
	command.Env = append(os.Environ(), "SUDO_UID=", "SUDO_GID=")
	if e = command.Run(); e == nil {
		t.Fatal("disposable owner elevation was not disabled")
	}
}
