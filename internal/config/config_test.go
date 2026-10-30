package config

import (
	"runtime"
	"strings"
	"testing"
)

func TestDecode(t *testing.T) {
	c, e := Decode(strings.NewReader("version: 1\ndns:\n  zones: [test]\ninstallations:\n  - domain: INSTALL.WEB.TEST.\n    target: 127.0.0.1:9001\n"))
	if e != nil {
		t.Fatal(e)
	}
	if c.Proxy.Listen != "127.0.0.42:80" || c.Installations[0].Domain != "install.web.test" {
		t.Fatal(c)
	}
}
func TestUnknownKey(t *testing.T) {
	if _, e := Decode(strings.NewReader("version: 1\ntypo: true\n")); e == nil {
		t.Fatal("accepted unknown key")
	}
}

func TestInvalidConfigurations(t *testing.T) {
	base := "version: 1\ndns:\n  zones: [test]\ninstallations:\n  - domain: install.web.test\n    target: 127.0.0.1:9001\n"
	for _, v := range []struct{ name, old, new string }{
		{"version", "version: 1", "version: 2"},
		{"unknown", "version: 1", "version: 1\nunknown: true"},
		{"local", "zones: [test]", "zones: [local]"},
		{"duplicate-zone", "zones: [test]", "zones: [test, TEST.]"},
		{"out-of-zone", "install.web.test", "install.web.example"},
		{"unicode", "install.web.test", "café.test"},
		{"invalid-label", "install.web.test", "bad-.test"},
		{"hostname-target", "127.0.0.1:9001", "localhost:9001"},
		{"lan-target", "127.0.0.1:9001", "192.168.1.2:80"},
		{"ipv6-target", "127.0.0.1:9001", "[::1]:80"},
		{"zero-port", "127.0.0.1:9001", "127.0.0.1:0"},
		{"duplicate-route", "target: 127.0.0.1:9001", "target: 127.0.0.1:9001\n  - domain: INSTALL.WEB.TEST.\n    target: 127.0.0.1:9002"},
		{"public-listen", "zones: [test]", "zones: [test]\n  listen: 0.0.0.0:53"},
		{"stub-listen", "zones: [test]", "zones: [test]\n  listen: 127.0.0.53:53"},
		{"localhost-listen", "zones: [test]", "zones: [test]\n  listen: 127.0.0.1:53"},
		{"same-address", "zones: [test]", "zones: [test]\n  listen: 127.0.0.42:53"},
	} {
		t.Run(v.name, func(t *testing.T) {
			if _, e := Decode(strings.NewReader(strings.Replace(base, v.old, v.new, 1))); e == nil {
				t.Fatal("accepted invalid configuration")
			}
		})
	}
	for _, s := range []string{base + "---\nversion: 1", base + "---\n", base + "dns: {}\n"} {
		if _, e := Decode(strings.NewReader(s)); e == nil {
			t.Fatal("accepted multiple documents or duplicate keys")
		}
	}
}
func FuzzNormalize(f *testing.F) {
	for _, s := range []string{"install.test", "DEEP.site.test.", "eviltest", "-bad.test", "a..test", "café.test"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		n, e := Normalize(s)
		if e != nil {
			return
		}
		again, e := Normalize(n)
		if e != nil || again != n {
			t.Fatalf("non-idempotent normalization: %q", s)
		}
	})
}

func TestUnicodeCaseFoldRejected(t *testing.T) {
	if _, e := Normalize("Kelvin.test"); e == nil {
		t.Fatal("Unicode was accepted through ASCII case folding")
	}
}
func TestNonCanonicalPort(t *testing.T) {
	for _, s := range []string{"127.0.0.1:+80", "127.0.0.1:080"} {
		if _, e := address(s); e == nil {
			t.Fatalf("accepted non-canonical port %s", s)
		}
	}
}

func TestWildcardCoexistenceIsExplicitAndMacOnly(t *testing.T) {
	ordinary, e := Decode(strings.NewReader("version: 1\ndns:\n  zones: [test]\n"))
	if e != nil {
		t.Fatal(e)
	}
	if ordinary.Proxy.AllowWildcardCoexistence {
		t.Fatal("wildcard coexistence enabled by default")
	}
	c, e := Decode(strings.NewReader("version: 1\ndns:\n  zones: [test]\nproxy:\n  allow_wildcard_coexistence: true\n"))
	if runtime.GOOS == "darwin" {
		if e != nil || !c.Proxy.AllowWildcardCoexistence {
			t.Fatalf("macOS opt-in: %+v, %v", c.Proxy, e)
		}
	} else if e == nil {
		t.Fatal("accepted macOS-only workaround on another platform")
	}
}
