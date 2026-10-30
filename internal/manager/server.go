package manager

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

	"github.com/cbspire/raymond/internal/control"
	"github.com/cbspire/raymond/internal/install"
)

func managerLock() (*os.File, error) {
	path := filepath.Join(control.RootDir, "manager.lock")
	if e := install.Trusted(path); e != nil {
		return nil, e
	}
	f, e := os.OpenFile(path, os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0600)
	if e != nil {
		return nil, e
	}
	st, e := f.Stat()
	if e != nil {
		f.Close()
		return nil, e
	}
	if !st.Mode().IsRegular() || st.Mode().Perm()&0077 != 0 {
		f.Close()
		return nil, fmt.Errorf("manager lock must be private regular file")
	}
	if e = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); e != nil {
		f.Close()
		return nil, fmt.Errorf("management daemon already running: %w", e)
	}
	return f, nil
}
func Run(ctx context.Context, version string) error {
	if os.Geteuid() != 0 {
		return fmt.Errorf("manager requires root")
	}
	m, e := install.ManagementMetadata()
	if e != nil {
		return e
	}
	lock, e := managerLock()
	if e != nil {
		return e
	}
	defer lock.Close()
	if e = install.Trusted(control.SocketDir); e != nil {
		return e
	}
	if e = os.MkdirAll(control.SocketDir, 0755); e != nil {
		return e
	}
	if e = os.Chmod(control.SocketDir, 0755); e != nil {
		return e
	}
	if st, err := os.Lstat(control.SocketPath); err == nil {
		if st.Mode()&os.ModeSocket == 0 {
			return fmt.Errorf("refusing non-socket control path")
		}
		if e = install.Trusted(control.SocketPath); e != nil {
			return e
		}
		if e = os.Remove(control.SocketPath); e != nil {
			return e
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	if e = install.Trusted(filepath.Join(control.RootDir, "sessions.json")); e != nil {
		return e
	}
	auth, e := control.NewAuth(filepath.Join(control.RootDir, "sessions.json"))
	if e != nil {
		return e
	}
	backend := NewBackend(m, version)
	if _, e = control.SnapshotOperations(control.RootDir); e != nil {
		return e
	}
	listener, e := net.Listen("unix", control.SocketPath)
	if e != nil {
		return e
	}
	defer listener.Close()
	defer os.Remove(control.SocketPath)
	// DynamicUser has no stable group. Its separately delivered credential grants
	// browser-only access; owner endpoints additionally verify Unix peer credentials.
	if e = os.Chmod(control.SocketPath, 0666); e != nil {
		return e
	}
	server := control.UnixServer(control.Handler(backend, auth, m.OwnerToken, m.ProxyToken), m.OwnerToken, m.UID)
	server.BaseContext = func(net.Listener) context.Context { return ctx }
	defer server.Close()
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case e = <-done:
			return e
		case <-ctx.Done():
			stop, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			e = server.Shutdown(stop)
			cancel()
			return e
		case <-ticker.C:
			if e = backend.recoverWorker(); e != nil {
				return errors.Join(e, server.Close())
			}
		}
	}
}
func spawnWorker(j *control.Operations) error {
	f, e := j.WorkerLock()
	if e != nil {
		return e
	}
	defer f.Close()
	if e = install.Trusted(install.ManagedBinary()); e != nil {
		return e
	}
	cmd := exec.Command(install.ManagedBinary(), "_operation")
	cmd.ExtraFiles = []*os.File{f}
	cmd.Env = []string{"PATH=/usr/bin:/bin:/usr/sbin:/sbin"}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if e = cmd.Start(); e != nil {
		return e
	}
	go func() { _ = cmd.Wait() }()
	return nil
}
