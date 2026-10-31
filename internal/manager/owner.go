// Package manager implements the local privileged broker and detached workers.
package manager

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/cbspire/raymond/internal/config"
	"github.com/cbspire/raymond/internal/control"
	"github.com/cbspire/raymond/internal/install"
	"golang.org/x/sys/unix"
)

type OwnerRequest struct {
	Action   string         `json:"action"`
	Path     string         `json:"path"`
	Revision string         `json:"revision,omitempty"`
	Config   *config.Config `json:"config,omitempty"`
	Data     []byte         `json:"data,omitempty"`
}
type OwnerResult struct {
	Data     []byte `json:"data,omitempty"`
	Revision string `json:"revision,omitempty"`
}

// All user-controlled paths are opened only after permanently dropping UID/GID.
// These helpers never select privileged paths, commands or downloads for the broker.
func OwnerHelper(input io.Reader, output io.Writer) error {
	if os.Geteuid() == 0 {
		return fmt.Errorf("owner helper requires an ordinary account")
	}
	var r OwnerRequest
	decoder := json.NewDecoder(io.LimitReader(input, 384<<20))
	decoder.DisallowUnknownFields()
	if e := decoder.Decode(&r); e != nil {
		return e
	}
	var extra any
	if e := decoder.Decode(&extra); e != io.EOF {
		return fmt.Errorf("expected one owner request")
	}
	result := OwnerResult{}
	switch r.Action {
	case "config-read":
		b, e := config.OwnedDocument(r.Path)
		if e != nil {
			return e
		}
		result.Data = b
		result.Revision = config.Revision(b)
	case "config-save", "config-discard":
		if r.Config == nil {
			return fmt.Errorf("configuration required")
		}
		save := config.SaveOwned
		if r.Action == "config-discard" {
			save = config.DiscardOwned
		}
		rev, e := save(r.Path, r.Revision, *r.Config)
		if e != nil {
			return e
		}
		result.Revision = rev
	case "cli-read":
		b, e := ownedBytes(r.Path, 256<<20, false)
		if e != nil {
			return e
		}
		result.Data = b
		result.Revision = config.Revision(b)
	case "cli-save":
		b, e := ownedBytes(r.Path, 256<<20, false)
		if e != nil {
			return e
		}
		if config.Revision(b) != r.Revision {
			return fmt.Errorf("CLI changed externally; refusing replacement")
		}
		if len(r.Data) == 0 || len(r.Data) > 256<<20 {
			return fmt.Errorf("invalid CLI size")
		}
		if e = saveCLI(r.Path, r.Revision, r.Data); e != nil {
			return e
		}
		result.Revision = config.Revision(r.Data)
	case "credential-clear":
		home, e := os.UserHomeDir()
		if e != nil {
			return e
		}
		path := filepath.Join(home, ".raymond", "control.json")
		data, e := ownedBytes(path, 64<<10, true)
		if os.IsNotExist(e) {
			break
		}
		if e != nil {
			return e
		}
		var access struct {
			Token string `json:"token"`
		}
		if e = json.Unmarshal(data, &access); e != nil {
			return e
		}
		if !control.Equal(access.Token, string(r.Data)) {
			return fmt.Errorf("owner credential changed; refusing cleanup")
		}
		if e = os.Remove(path); e != nil {
			return e
		}
	case "credential", "launch":
		home, e := os.UserHomeDir()
		if e != nil {
			return e
		}
		dir := filepath.Join(home, ".raymond")
		if e = os.MkdirAll(dir, 0700); e != nil {
			return e
		}
		st, e := os.Lstat(dir)
		if e != nil {
			return e
		}
		identity, ok := st.Sys().(*syscall.Stat_t)
		if !ok || !st.IsDir() || int(identity.Uid) != os.Geteuid() || st.Mode().Perm() != 0700 {
			return fmt.Errorf("owner credential directory must be private and owned")
		}
		name := "control.json"
		if r.Action == "launch" {
			name = "dashboard-launch.html"
		}
		path := filepath.Join(dir, name)
		if e = install.SafePath(path); e != nil {
			return e
		}
		if st, e = os.Lstat(path); e == nil {
			if !st.Mode().IsRegular() || int(st.Sys().(*syscall.Stat_t).Uid) != os.Geteuid() || st.Mode().Perm()&0077 != 0 {
				return fmt.Errorf("unsafe owner credential file")
			}
		} else if !os.IsNotExist(e) {
			return e
		}
		if len(r.Data) > 64<<10 {
			return fmt.Errorf("owner credential too large")
		}
		if e = control.AtomicWrite(path, r.Data, 0600); e != nil {
			return e
		}
	case "check-cli":
		if _, e := ownedBytes(r.Path, 256<<20, false); e != nil {
			return e
		}
		if e := unix.Access(filepath.Dir(r.Path), unix.W_OK|unix.X_OK); e != nil {
			return fmt.Errorf("CLI must be in an invoking-user writable directory: %w", e)
		}
	default:
		return fmt.Errorf("unknown owner helper action")
	}
	return json.NewEncoder(output).Encode(result)
}
func ownedBytes(path string, limit int64, private bool) ([]byte, error) {
	if e := install.SafePath(path); e != nil {
		return nil, e
	}
	f, e := os.OpenFile(path, os.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if e != nil {
		return nil, e
	}
	defer f.Close()
	st, e := f.Stat()
	if e != nil {
		return nil, e
	}
	identity, ok := st.Sys().(*syscall.Stat_t)
	mask := os.FileMode(0022)
	if private {
		mask = 0077
	}
	if !ok || !st.Mode().IsRegular() || int(identity.Uid) != os.Geteuid() || st.Mode().Perm()&mask != 0 || identity.Nlink != 1 {
		return nil, fmt.Errorf("expected an owned regular private file")
	}
	b, e := io.ReadAll(io.LimitReader(f, limit+1))
	if e != nil {
		return nil, e
	}
	if int64(len(b)) > limit {
		return nil, fmt.Errorf("owner file too large")
	}
	return b, nil
}

type limitedBuffer struct {
	bytes.Buffer
	limit int
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if len(p) > b.limit-b.Len() {
		return 0, fmt.Errorf("helper output too large")
	}
	return b.Buffer.Write(p)
}
func OwnerCall(ctx context.Context, executable string, m control.Metadata, r OwnerRequest) (OwnerResult, error) {
	var result OwnerResult
	// The all-ones value is also reserved by setuid/setgid as “leave unchanged”.
	if m.UID <= 0 || m.UID >= math.MaxUint32 || m.GID < 0 || m.GID >= math.MaxUint32 {
		return result, fmt.Errorf("management requires an ordinary enrolled owner")
	}
	if e := install.Trusted(executable); e != nil {
		return result, e
	}
	return ownerCall(ctx, executable, m, r)
}

// Initial enrollment has explicitly authorized the source executable already;
// it is executed only as its ordinary owner, never again as root.
func EnrollOwner(ctx context.Context, executable string, m control.Metadata, r OwnerRequest) (OwnerResult, error) {
	return ownerCall(ctx, executable, m, r)
}
func ownerCall(ctx context.Context, executable string, m control.Metadata, r OwnerRequest) (OwnerResult, error) {
	var result OwnerResult
	// The all-ones value is also reserved by setuid/setgid as “leave unchanged”.
	if m.UID <= 0 || m.UID >= math.MaxUint32 || m.GID < 0 || m.GID >= math.MaxUint32 {
		return result, fmt.Errorf("invalid enrolled account")
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	b, e := json.Marshal(r)
	if e != nil {
		return result, e
	}
	cmd := exec.CommandContext(ctx, executable, "_owner-io")
	cmd.Env = []string{"HOME=" + m.Home, "PATH=/usr/bin:/bin:/usr/sbin:/sbin", "USER=" + strconv.Itoa(m.UID)}
	cmd.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: uint32(m.UID), Gid: uint32(m.GID), Groups: []uint32{uint32(m.GID)}}}
	cmd.Stdin = bytes.NewReader(b)
	outputLimit := 64 << 10
	if r.Action == "config-read" {
		outputLimit = 2 << 20
	}
	if r.Action == "cli-read" {
		outputLimit = 384 << 20
	}
	out := &limitedBuffer{limit: outputLimit}
	errout := &limitedBuffer{limit: 8192}
	cmd.Stdout = out
	cmd.Stderr = errout
	if e = cmd.Run(); e != nil {
		return result, fmt.Errorf("owner file operation failed: %w: %s", e, errout.String())
	}
	e = json.Unmarshal(out.Bytes(), &result)
	return result, e
}

// Recheck the owner's executable immediately before publication, after the new
// bytes have been synced. Developer builds made during an update must not be lost.
func saveCLI(path, expected string, data []byte) error {
	f, e := os.CreateTemp(filepath.Dir(path), ".raymond-cli-*")
	if e != nil {
		return e
	}
	defer os.Remove(f.Name())
	if e = f.Chmod(0755); e == nil {
		_, e = f.Write(data)
	}
	if e == nil {
		e = f.Sync()
	}
	closed := f.Close()
	if e == nil {
		e = closed
	}
	if e != nil {
		return e
	}
	current, e := ownedBytes(path, 256<<20, false)
	if e != nil {
		return e
	}
	if config.Revision(current) != expected {
		return fmt.Errorf("CLI changed during replacement; refusing overwrite")
	}
	if e = os.Rename(f.Name(), path); e != nil {
		return e
	}
	directory, e := os.Open(filepath.Dir(path))
	if e != nil {
		return e
	}
	defer directory.Close()
	return directory.Sync()
}
