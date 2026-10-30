// Package diagnostics performs non-destructive bind and resolver checks.
package diagnostics

import (
	"fmt"
	"github.com/cbspire/raymond/internal/config"
	"net"
)

// CheckWildcardListeners retains the conservative default wildcard policy.
// An explicit HTTP opt-in permits Darwin's separate-address socket coexistence;
// DNS conflicts and the real TCP/UDP bind checks are never bypassed.
func CheckWildcardListeners(c config.Config) error {
	if !c.Proxy.AllowWildcardCoexistence {
		if e := checkWildcard(c.Proxy.Listen); e != nil {
			return e
		}
	}
	return checkWildcard(c.DNS.Listen)
}

func Check(c config.Config) error {
	if e := CheckWildcardListeners(c); e != nil {
		return e
	}
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
