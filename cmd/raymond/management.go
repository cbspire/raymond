package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"syscall"
	"time"

	"github.com/cbspire/raymond/internal/config"
	"github.com/cbspire/raymond/internal/control"
	"github.com/cbspire/raymond/internal/install"
	"github.com/cbspire/raymond/internal/manager"
	"github.com/cbspire/raymond/internal/release"
	"github.com/cbspire/raymond/internal/router"
)

func waitOperation(ctx context.Context, client *control.Client, request control.Request) error {
	var op control.Operation
	if e := client.Call(ctx, http.MethodPost, control.Prefix+"operations", request, &op); e != nil {
		return e
	}
	for op.Status == "queued" || op.Status == "running" {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(300 * time.Millisecond):
		}
		probe, cancel := context.WithTimeout(ctx, 3*time.Second)
		e := client.Call(probe, http.MethodGet, control.Prefix+"operations/"+url.PathEscape(op.ID), nil, &op)
		cancel()
		if e != nil && ctx.Err() != nil {
			return ctx.Err()
		}
	}
	if op.Status != "succeeded" {
		if op.Error == "" {
			return fmt.Errorf("operation failed")
		}
		return fmt.Errorf("%s", op.Error)
	}
	if (request.Action == "diagnostics" || request.Action == "check-update") && len(op.Result) > 0 {
		var formatted bytes.Buffer
		if json.Indent(&formatted, op.Result, "", "  ") == nil {
			fmt.Println(formatted.String())
		}
	}
	return nil
}
func managedCommand(client *control.Client, request control.Request) error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	defer client.HTTP.CloseIdleConnections()
	if e := waitOperation(ctx, client, request); e != nil {
		return e
	}
	fmt.Printf("Raymond %s completed.\n", request.Action)
	return nil
}
func dashboardCommand(args []string) error {
	if len(args) != 0 || os.Geteuid() == 0 {
		return fmt.Errorf("run raymond dashboard as the enrolled ordinary account, without arguments")
	}
	client, e := control.OwnerClient()
	if e != nil {
		return fmt.Errorf("dashboard access unavailable; run raymond setup: %w", e)
	}
	return openDashboard(client, openBrowser)
}

// Both setup and upgrades use the same authorized launch path as the optional
// dashboard shortcut. The opener receives only a private file URL, never a token.
func openDashboard(client *control.Client, opener func(context.Context, string) error) error {
	var e error
	defer client.HTTP.CloseIdleConnections()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var state control.State
	if e = client.Call(ctx, "GET", control.Prefix+"state", nil, &state); e != nil {
		return e
	}
	if _, matched := router.New(state.Active.Config.Routes).Match(router.DashboardHost); matched {
		return fmt.Errorf("a configured application route shadows raymond.test; the dashboard cannot override it")
	}
	covered := false
	for _, zone := range state.Active.Config.DNS.Zones {
		covered = covered || config.Within(router.DashboardHost, zone)
	}
	if !covered {
		return fmt.Errorf("dashboard hostname is outside the active DNS zones")
	}
	if !state.Running {
		if e = waitOperation(ctx, client, control.Request{Action: "start"}); e != nil {
			return e
		}
	}
	var launch struct {
		Token string `json:"token"`
	}
	if e = client.Call(ctx, "POST", control.Prefix+"launch", nil, &launch); e != nil {
		return e
	}
	home, e := os.UserHomeDir()
	if e != nil {
		return e
	}
	// Open a private local redirect file so the single-use token is not exposed in
	// a process argument, terminal output, HTTP query string or access log.
	target := control.Origin + "/#token=" + url.QueryEscape(launch.Token)
	html := []byte(`<!doctype html><meta http-equiv="refresh" content="0;url=` + target + `"><title>Opening Raymond</title>`)
	request, _ := json.Marshal(manager.OwnerRequest{Action: "launch", Data: html})
	if e = manager.OwnerHelper(bytes.NewReader(request), io.Discard); e != nil {
		return e
	}
	fileURL := (&url.URL{Scheme: "file", Path: filepath.Join(home, ".raymond", "dashboard-launch.html")}).String()
	return opener(ctx, fileURL)
}

func openBrowser(ctx context.Context, fileURL string) error {
	command := "/usr/bin/xdg-open"
	if runtime.GOOS == "darwin" {
		command = "/usr/bin/open"
	}
	if _, e := os.Stat(command); e != nil || (runtime.GOOS == "linux" && os.Getenv("DISPLAY") == "" && os.Getenv("WAYLAND_DISPLAY") == "") {
		fmt.Println("Dashboard access is enrolled. Open raymond dashboard in a desktop session to launch an authorized browser.")
		return nil
	}
	cmd := exec.CommandContext(ctx, command, fileURL)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if e := cmd.Run(); e != nil {
		return fmt.Errorf("browser could not be opened; run raymond dashboard in a desktop session")
	}
	return nil
}

func enrollSetup(path string) error {
	executable, e := os.Executable()
	if e != nil {
		return e
	}
	var source, document manager.OwnerResult
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	m, e := install.PrepareManagement(path, func(m control.Metadata) error {
		var err error
		if _, err = manager.EnrollOwner(ctx, executable, m, manager.OwnerRequest{Action: "check-cli", Path: m.CLIPath}); err != nil {
			return err
		}
		document, err = manager.EnrollOwner(ctx, executable, m, manager.OwnerRequest{Action: "config-read", Path: m.ConfigPath})
		if err != nil {
			return err
		}
		if _, err = config.Decode(bytes.NewReader(document.Data)); err != nil {
			return err
		}
		source, err = manager.EnrollOwner(ctx, executable, m, manager.OwnerRequest{Action: "cli-read", Path: executable})
		return err
	})
	if e != nil {
		return e
	}
	journal, e := control.OpenOperations(control.RootDir)
	if e != nil {
		return e
	}
	defer journal.Close()
	if active := journal.Active(); active != nil {
		if active.Status == "queued" {
			if e = journal.Start(active.ID); e != nil {
				return e
			}
			if e = journal.Finish(active.ID, nil, fmt.Errorf("enrollment interrupted before execution")); e != nil {
				return e
			}
		} else {
			fd, err := journal.WorkerLock()
			if err != nil {
				return err
			}
			journal.Close()
			if err = manager.Worker(version, fd); err != nil {
				return err
			}
			journal, e = control.OpenOperations(control.RootDir)
			if e != nil {
				return e
			}
			defer journal.Close()
		}
	}
	stage := filepath.Join(control.RootDir, "enrollment")
	if e = install.Trusted(stage); e != nil {
		return e
	}
	if e = os.MkdirAll(stage, 0700); e != nil {
		return e
	}
	defer os.RemoveAll(stage)
	if e = control.AtomicWrite(stage+"/config.yaml", document.Data, 0600); e != nil {
		return e
	}
	if e = control.AtomicWrite(stage+"/raymond", source.Data, 0700); e != nil {
		return e
	}

	if e = enrollNative(ctx, m, stage, document.Revision, journal); e != nil {
		return e
	}

	client := control.NewClient(control.SocketPath, m.OwnerToken)
	defer client.HTTP.CloseIdleConnections()
	deadline := time.Now().Add(15 * time.Second)
	for {
		var state control.State
		probe, stop := context.WithTimeout(ctx, time.Second)
		e = client.Call(probe, "GET", control.Prefix+"state", nil, &state)
		stop()
		if e == nil {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("management service did not become ready: %w", e)
		}
		time.Sleep(100 * time.Millisecond)
	}
}
func setupManaged(client *control.Client, path string) error {
	var e error
	if path != "" {
		path, e = filepath.Abs(path)
		if e != nil {
			return e
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	defer client.HTTP.CloseIdleConnections()
	var state control.State
	if e = client.Call(ctx, "GET", control.Prefix+"state", nil, &state); e != nil {
		return e
	}
	if path == "" {
		path = state.ConfigPath
	}
	if path != state.ConfigPath {
		return fmt.Errorf("management is enrolled for a different configuration path")
	}
	action := control.Request{Action: "retry"}
	if release.Newer(version, state.Version) {
		action = control.Request{Action: "update", Version: version}
	}
	return waitOperation(ctx, client, action)
}
func managedLogs(ctx context.Context, client *control.Client, options install.LogOptions) error {
	defer client.HTTP.CloseIdleConnections()
	path := control.Prefix + "logs?lines=" + strconv.Itoa(options.Lines) + "&follow=" + strconv.FormatBool(options.Follow)
	req, e := http.NewRequestWithContext(ctx, "GET", "http://control"+path, nil)
	if e != nil {
		return e
	}
	req.Header.Set("Authorization", "Bearer "+client.Token)
	response, e := client.HTTP.Do(req)
	if e != nil {
		return e
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return fmt.Errorf("management logs unavailable (HTTP %d)", response.StatusCode)
	}
	_, e = io.Copy(os.Stdout, response.Body)
	if ctx.Err() != nil {
		return nil
	}
	return e
}
func rootManagedClient() (*control.Client, error) {
	m, e := install.ManagementMetadata()
	if e != nil {
		return nil, e
	}
	if raw := os.Getenv("SUDO_UID"); raw != "" && raw != "0" {
		uid, e := strconv.Atoi(raw)
		if e != nil || uid != m.UID {
			return nil, fmt.Errorf("management belongs to another local account")
		}
	}
	return control.NewClient(control.SocketPath, m.OwnerToken), nil
}

// Initial enrollment uses the same persistent transaction format as workers.
// Once the protected candidate is published, the manager can recover an orphaned
// enrollment after the invoking CLI disappears, without rerunning user commands.
func enrollNative(ctx context.Context, m control.Metadata, stage, revision string, journal *control.Operations) error {
	unlock, e := install.ManagedLock()
	if e != nil {
		return e
	}
	defer unlock()
	op, e := journal.Enqueue(control.Request{Action: "retry"})
	if e != nil {
		return e
	}
	if e = journal.Start(op.ID); e != nil {
		return e
	}
	active, _ := manager.NewBackend(m, version).Active()
	transaction, e := install.PrepareManaged(op.ID, active.Revision, stage+"/raymond", nil, true)
	if e != nil {
		_ = journal.Finish(op.ID, nil, e)
		if _, err := install.LoadTransaction(); os.IsNotExist(err) {
			_ = install.ClearTransaction()
		}
		return e
	}
	fail := func(original error) error {
		if rollback := install.RollbackManaged(transaction); rollback != nil {
			return fmt.Errorf("enrollment failed and rollback evidence was retained: %w", errors.Join(original, rollback))
		}
		if !transaction.NewInstall {
			_ = manager.MarkActive(transaction.Revision)
		}
		if e = journal.Finish(op.ID, nil, original); e != nil {
			return e
		}
		if !transaction.NewInstall {
			_ = install.ClearTransaction()
		}
		return fmt.Errorf("enrollment failed; previous service restored: %w", original)
	}
	if e = install.PublishBootstrap(transaction, stage+"/raymond"); e != nil {
		return fail(e)
	}
	if e = install.StartManagement(false); e != nil {
		return fail(e)
	}
	if e = install.ApplyManaged(transaction, stage+"/config.yaml", stage+"/raymond"); e != nil {
		return fail(e)
	}
	if e = manager.CheckHealth(ctx); e != nil {
		return fail(e)
	}
	if e = manager.MarkActive(revision); e != nil {
		return fail(e)
	}
	if e = install.CommitManaged(transaction); e != nil {
		return e
	}
	// Committed enrollment is completed, never rolled back, after interruption.
	if e = manager.FinishEnrollment(ctx, m); e != nil {
		return e
	}
	if e = journal.Finish(op.ID, nil, nil); e != nil {
		return e
	}
	return install.ClearTransaction()
}

func uninstallCommand() error {
	if os.Geteuid() != 0 {
		return fmt.Errorf("uninstall removes OS resources; explicitly run with sudo")
	}
	metadata, err := install.ManagementMetadata()
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if err != nil {
		return install.Uninstall()
	}
	// Retain a reviewed helper outside the resources about to be removed. It runs
	// only with the enrolled user's UID, and can delete only its matching credential.
	if _, e := install.Installed(); e != nil && !os.IsNotExist(e) {
		return e
	}
	if e := install.Trusted(install.ManagedBinary()); e != nil {
		return e
	}
	source, e := os.OpenFile(install.ManagedBinary(), os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if os.IsNotExist(e) {
		return install.Uninstall()
	}
	if e != nil {
		return e
	}
	st, e := source.Stat()
	if e != nil {
		source.Close()
		return e
	}
	if !st.Mode().IsRegular() || st.Mode().Perm()&0111 == 0 {
		source.Close()
		return fmt.Errorf("uninstall helper must be a regular executable")
	}
	data, e := io.ReadAll(io.LimitReader(source, (256<<20)+1))
	source.Close()
	if e != nil {
		return e
	}
	if len(data) > 256<<20 {
		return fmt.Errorf("uninstall helper too large")
	}
	if _, err := os.Stat("/var/lib/raymond/state.json"); os.IsNotExist(err) {
		transaction, err := install.LoadTransaction()
		if err != nil {
			return err
		}
		hash := config.Revision(data)
		if !transaction.NewInstall || (hash != transaction.NewBinaryHash && hash != transaction.BinaryHash) {
			return fmt.Errorf("unproven bootstrap binary")
		}
	}
	dir, e := os.MkdirTemp("", "raymond-uninstall-")
	if e != nil {
		return e
	}
	defer os.RemoveAll(dir)
	if e = os.Chmod(dir, 0755); e != nil {
		return e
	}
	helper := filepath.Join(dir, "raymond")
	if e = os.WriteFile(helper, data, 0755); e != nil {
		return e
	}
	if e = install.Uninstall(); e != nil {
		return e
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	// A failed migration may have restored a pre-management protected binary.
	// Prefer the explicitly invoked CLI when it remains outside managed resources;
	// it is executed only after permanently dropping to the enrolled account.
	if executable, err := os.Executable(); err == nil && executable != install.ManagedBinary() {
		helper = executable
	}
	_, e = manager.EnrollOwner(ctx, helper, metadata, manager.OwnerRequest{Action: "credential-clear", Data: []byte(metadata.OwnerToken)})
	return e
}
