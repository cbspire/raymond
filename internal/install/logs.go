package install

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"syscall"

	"github.com/cbspire/raymond/internal/platform/darwin"
)

type LogOptions struct {
	Lines  int
	Follow bool
}

func (o LogOptions) Validate() error {
	if o.Lines < 0 || o.Lines > 10000 {
		return fmt.Errorf("logs --lines must be between 0 and 10000")
	}
	return nil
}

func logCommand(platform string, options LogOptions) (string, []string, error) {
	if e := options.Validate(); e != nil {
		return "", nil, e
	}
	count := strconv.Itoa(options.Lines)
	switch platform {
	case "darwin":
		args := []string{"-n", count}
		if options.Follow {
			args = append(args, "-F")
		}
		return "/usr/bin/tail", append(args, darwin.LogPath), nil
	case "linux":
		args := []string{"--unit", "raymond.service", "--lines", count, "--no-pager", "--output", "short-iso"}
		if options.Follow {
			args = append(args, "--follow")
		}
		return "/usr/bin/journalctl", args, nil
	default:
		return "", nil, fmt.Errorf("logs are unsupported on %s", platform)
	}
}

// Logs streams the installed service's fixed log destination. It deliberately
// avoids the short lifecycle-command timeout because following is long-lived.
func Logs(ctx context.Context, options LogOptions) error {
	return StreamLogs(ctx, options, os.Stdout, os.Stderr)
}

func StreamLogs(ctx context.Context, options LogOptions, stdout, stderr io.Writer) error {
	name, args, e := logCommand(runtime.GOOS, options)
	if e != nil {
		return e
	}
	if runtime.GOOS == "darwin" {
		f, e := os.OpenFile(darwin.LogPath, os.O_RDONLY|syscall.O_NONBLOCK|syscall.O_NOFOLLOW, 0)
		if e != nil {
			return fmt.Errorf("cannot read Raymond logs: %w; install and start the service first", e)
		}
		st, e := f.Stat()
		f.Close()
		if e != nil {
			return e
		}
		if !st.Mode().IsRegular() {
			return fmt.Errorf("service log must be a regular file")
		}
	}
	return streamLogs(ctx, name, args, stdout, stderr)
}

func streamLogs(ctx context.Context, name string, args []string, stdout, stderr io.Writer) error {
	command := exec.CommandContext(ctx, name, args...)
	command.Stdout = stdout
	command.Stderr = stderr
	e := command.Run()
	if ctx.Err() != nil {
		return nil
	}
	if e != nil {
		return fmt.Errorf("read Raymond service logs: %w", e)
	}
	return nil
}
