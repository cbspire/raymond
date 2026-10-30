package diagnostics

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os/exec"
	"time"
)

// BSD can permit a wildcard and a specific-address socket to coexist under
// SO_REUSEADDR. Bind probes alone therefore cannot identify this conflict.
func checkWildcard(address string) error {
	_, port, e := net.SplitHostPort(address)
	if e != nil {
		return e
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	b, e := exec.CommandContext(ctx, "/usr/sbin/lsof", "-nP", "-iTCP:"+port, "-sTCP:LISTEN", "-F", "n").CombinedOutput()
	if e != nil {
		var exit *exec.ExitError
		if !errors.As(e, &exit) || exit.ExitCode() != 1 || len(b) != 0 {
			return fmt.Errorf("cannot inspect wildcard listeners; run doctor explicitly with administrator privileges: %w", e)
		}
	}
	if wildcardOutput(b) {
		return fmt.Errorf("wildcard TCP listener conflicts with %s; leave the other service running and reconfigure it explicitly", address)
	}
	return nil
}
