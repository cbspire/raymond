package install

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestLogsCommandFollowsOnlyRaymond(t *testing.T) {
	for _, tc := range []struct {
		platform string
		follow   bool
		command  string
		args     []string
	}{
		{"darwin", true, "/usr/bin/tail", []string{"-n", "50", "-F", "/var/log/raymond.log"}},
		{"darwin", false, "/usr/bin/tail", []string{"-n", "50", "/var/log/raymond.log"}},
		{"linux", true, "/usr/bin/journalctl", []string{"--unit", "raymond.service", "--lines", "50", "--no-pager", "--output", "short-iso", "--follow"}},
		{"linux", false, "/usr/bin/journalctl", []string{"--unit", "raymond.service", "--lines", "50", "--no-pager", "--output", "short-iso"}},
	} {
		name, args, e := logCommand(tc.platform, LogOptions{Lines: 50, Follow: tc.follow})
		if e != nil || name != tc.command || !reflect.DeepEqual(args, tc.args) {
			t.Fatalf("%s logs: %s %v, %v", tc.platform, name, args, e)
		}
	}
	if _, _, e := logCommand("unsupported", LogOptions{Lines: 50}); e == nil {
		t.Fatal("accepted unsupported platform")
	}
	for _, lines := range []int{-1, 10001} {
		if _, _, e := logCommand("darwin", LogOptions{Lines: lines}); e == nil {
			t.Fatal("accepted invalid line count")
		}
	}
}

type logLineWriter struct {
	pending string
	lines   chan string
}

func (w *logLineWriter) Write(p []byte) (int, error) {
	w.pending += string(p)
	for {
		line, rest, ok := strings.Cut(w.pending, "\n")
		if !ok {
			break
		}
		w.pending = rest
		w.lines <- line
	}
	return len(p), nil
}

func TestLogsStreamsNewRequestsAndStopsOnCancel(t *testing.T) {
	path := filepath.Join(t.TempDir(), "raymond.log")
	if e := os.WriteFile(path, []byte("ready\n"), 0600); e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	output := &logLineWriter{lines: make(chan string, 8)}
	done := make(chan error, 1)
	go func() { done <- streamLogs(ctx, "/usr/bin/tail", []string{"-n", "1", "-F", path}, output, io.Discard) }()
	expect := func(want string) {
		t.Helper()
		select {
		case got := <-output.lines:
			if got != want {
				t.Fatalf("log line %q, want %q", got, want)
			}
		case <-time.After(4 * time.Second):
			t.Fatalf("timed out waiting for %q", want)
		}
	}
	expect("ready")
	f, e := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
	if e != nil {
		t.Fatal(e)
	}
	_, e = f.WriteString("request method=GET host=raymond.test status=200\n")
	f.Close()
	if e != nil {
		t.Fatal(e)
	}
	expect("request method=GET host=raymond.test status=200")
	cancel()
	select {
	case e := <-done:
		if e != nil {
			t.Fatal(e)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("log following did not stop")
	}
}
