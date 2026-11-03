package install

import (
	"github.com/cbspire/raymond/internal/config"
	"testing"
)

func TestServiceListenerPolicy(t *testing.T) {
	for _, tc := range []struct {
		name, platform, dns, proxy string
		want                       bool
	}{
		{"macOS default", "darwin", "127.0.0.43:53", "127.0.0.42:80", true},
		{"macOS CoreDNS coexistence", "darwin", "127.0.0.43:1053", "127.0.0.42:80", true},
		{"Linux default", "linux", "127.0.0.43:53", "127.0.0.42:80", true},
		{"Linux custom DNS port", "linux", "127.0.0.43:1053", "127.0.0.42:80", false},
		{"macOS custom DNS address", "darwin", "127.0.0.44:1053", "127.0.0.42:80", false},
		{"macOS custom proxy port", "darwin", "127.0.0.43:1053", "127.0.0.42:8080", false},
		{"macOS localhost DNS", "darwin", "127.0.0.1:1053", "127.0.0.42:80", false},
		{"macOS wildcard DNS", "darwin", "0.0.0.0:1053", "127.0.0.42:80", false},
		{"macOS malformed DNS", "darwin", "127.0.0.43", "127.0.0.42:80", false},
		{"macOS invalid DNS port", "darwin", "127.0.0.43:0", "127.0.0.42:80", false},
		{"unsupported platform", "windows", "127.0.0.43:53", "127.0.0.42:80", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := config.Config{Version: 1, DNS: config.DNS{Zones: []string{"internal"}, Listen: tc.dns}, Proxy: config.Proxy{Listen: tc.proxy}}
			if err := validateServiceListeners(c, tc.platform); (err == nil) != tc.want {
				t.Fatalf("accepted=%v want=%v: %v", err == nil, tc.want, err)
			}
		})
	}
}
