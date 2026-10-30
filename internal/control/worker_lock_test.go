package control

import (
	"bufio"
	"fmt"
	"golang.org/x/sys/unix"
	"io"
	"os"
	"os/exec"
	"testing"
)

func TestOperationLockSurvivesExec(t *testing.T) {
	if os.Getenv("RAYMOND_FD_TEST_CHILD") == "1" {
		file := os.NewFile(3, "worker lock")
		j, e := AdoptOperations(os.Getenv("RAYMOND_FD_TEST_DIR"), file)
		if e != nil {
			t.Fatal(e)
		}
		defer j.Close()
		active := j.Active()
		if active == nil {
			t.Fatal("missing work")
		}
		if e = j.Start(active.ID); e != nil {
			t.Fatal(e)
		}
		probe := exec.Command(os.Args[0], "-test.run=^TestOperationToolDoesNotInheritLock$")
		probe.Env = append(os.Environ(), "RAYMOND_FD_PROBE="+os.Getenv("RAYMOND_FD_TEST_DIR"))
		if output, e := probe.CombinedOutput(); e != nil {
			t.Fatalf("private FD inherited by tool: %v %s", e, output)
		}
		fmt.Println("ready")
		_, _ = io.Copy(io.Discard, os.Stdin)
		if e = j.Finish(active.ID, nil, nil); e != nil {
			t.Fatal(e)
		}
		return
	}
	dir := t.TempDir()
	j, e := OpenOperations(dir)
	if e != nil {
		t.Fatal(e)
	}
	defer j.Close()
	if _, e = j.Enqueue(Request{Action: "restart"}); e != nil {
		t.Fatal(e)
	}
	lock, e := j.WorkerLock()
	if e != nil {
		t.Fatal(e)
	}
	defer lock.Close()
	cmd := exec.Command(os.Args[0], "-test.run=^TestOperationLockSurvivesExec$")
	cmd.Env = append(os.Environ(), "RAYMOND_FD_TEST_CHILD=1", "RAYMOND_FD_TEST_DIR="+dir)
	cmd.ExtraFiles = []*os.File{lock}
	cmd.Stderr = os.Stderr
	input, e := cmd.StdinPipe()
	if e != nil {
		t.Fatal(e)
	}
	output, e := cmd.StdoutPipe()
	if e != nil {
		t.Fatal(e)
	}
	if e = cmd.Start(); e != nil {
		t.Fatal(e)
	}
	defer cmd.Process.Kill()
	if e = j.Close(); e != nil {
		t.Fatal(e)
	}
	if e = lock.Close(); e != nil {
		t.Fatal(e)
	}
	scanner := bufio.NewScanner(output)
	if !scanner.Scan() || scanner.Text() != "ready" {
		t.Fatal("worker failed to inherit lock")
	}
	if other, e := OpenOperations(dir); e == nil {
		other.Close()
		t.Fatal("new manager entered while worker alive")
	}
	snapshot, e := SnapshotOperations(dir)
	if e != nil || snapshot.Active() == nil || snapshot.Active().Status != "running" {
		t.Fatal("worker progress unavailable", e)
	}
	input.Close()
	if e = cmd.Wait(); e != nil {
		t.Fatal(e)
	}
	next, e := OpenOperations(dir)
	if e != nil {
		t.Fatal(e)
	}
	defer next.Close()
	if next.Active() != nil {
		t.Fatal("worker result not durable")
	}
}

func TestOperationToolDoesNotInheritLock(t *testing.T) {
	dir := os.Getenv("RAYMOND_FD_PROBE")
	if dir == "" {
		t.Skip("subprocess probe")
	}
	var descriptor, expected unix.Stat_t
	if e := unix.Stat(dir+"/operations.lock", &expected); e != nil {
		t.Fatal(e)
	}
	if e := unix.Fstat(3, &descriptor); e == nil && descriptor.Dev == expected.Dev && descriptor.Ino == expected.Ino {
		t.Fatal("root operation descriptor leaked to a tool")
	}
}
