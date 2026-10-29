// Package diagnostics performs non-destructive bind and resolver checks.
package diagnostics

import (
	"fmt"
	"github.com/cbspire/raymond/internal/config"
	"net"
)

func Check(c config.Config) error {
	for _, a := range []string{c.Proxy.Listen, c.DNS.Listen} {
		l, e := net.Listen("tcp4", a)
		if e != nil {
			return fmt.Errorf("cannot bind %s (conflict, missing loopback alias, or privilege): %w; leave existing services running", a, e)
		}
		l.Close()
	}
	u, e := net.ListenPacket("udp4", c.DNS.Listen)
	if e != nil {
		return fmt.Errorf("cannot bind DNS UDP: %w", e)
	}
	return u.Close()
}
