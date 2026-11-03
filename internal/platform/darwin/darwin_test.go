package darwin

import (
	"encoding/xml"
	"github.com/cbspire/raymond/internal/config"
	"strings"
	"testing"
)

func TestResources(t *testing.T) {
	f := Files(config.Config{DNS: config.DNS{Zones: []string{"test", "internal"}}})
	for _, z := range []string{"test", "internal"} {
		s := string(f["/etc/resolver/"+z])
		if !strings.HasPrefix(s, Marker) || !strings.Contains(s, "127.0.0.43") {
			t.Fatal(s)
		}
	}
	var v any
	if e := xml.Unmarshal(f["/Library/LaunchDaemons/dev.raymond.plist"], &v); e != nil {
		t.Fatal(e)
	}
}

func TestServiceUsesProtectedHelperPath(t *testing.T) {
	f := Files(config.Config{})
	plist := string(f["/Library/LaunchDaemons/dev.raymond.plist"])
	if !strings.Contains(plist, "<string>"+BinaryPath+"</string>") || strings.Contains(plist, "/usr/local/") {
		t.Fatal("launchd binary must use a protected helper path outside Homebrew directories")
	}
}

func TestResolverUsesConfiguredDNSPort(t *testing.T) {
	c := config.Config{Version: 1, MainDomain: "raymond.internal", DNS: config.DNS{Zones: []string{"internal"}, Listen: "127.0.0.43:1053"}}
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	files := Files(c)
	if got, want := string(files["/etc/resolver/internal"]), Marker+"nameserver 127.0.0.43\nport 1053\n"; got != want {
		t.Fatalf("resolver: %q, want %q", got, want)
	}
	if _, exists := files["/etc/resolver/test"]; exists {
		t.Fatal("generated an unrelated .test resolver")
	}
}
