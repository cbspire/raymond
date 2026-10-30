package diagnostics

import (
	"net"
	"strings"
)

// wildcardOutput parses lsof field output, never process command lines or names.
func wildcardOutput(data []byte) bool {
	for _, line := range strings.Split(string(data), "\n") {
		if !strings.HasPrefix(line, "n") {
			continue
		}
		host, _, e := net.SplitHostPort(strings.TrimPrefix(line, "n"))
		if e != nil {
			continue
		}
		if host == "*" || host == "" {
			return true
		}
		if ip := net.ParseIP(host); ip != nil && ip.IsUnspecified() {
			return true
		}
	}
	return false
}
