package linux

import (
	"github.com/cbspire/raymond/internal/config"
	"strings"
	"testing"
)

func TestIsolation(t *testing.T) {
	f := Files(config.Config{DNS: config.DNS{Zones: []string{"test", "internal"}}})
	n := string(f["/etc/systemd/network/80-raymond.network"])
	for _, s := range []string{"Name=raymond0", "DNS=127.0.0.43", "Domains=~test ~internal", "DNSDefaultRoute=no"} {
		if !strings.Contains(n, s) {
			t.Fatal(s)
		}
	}
	service := string(f["/etc/systemd/system/raymond.service"])
	for _, s := range []string{"DynamicUser=yes", "CapabilityBoundingSet=CAP_NET_BIND_SERVICE", "NoNewPrivileges=yes"} {
		if !strings.Contains(service, s) {
			t.Fatal(s)
		}
	}
	if _, ok := f["/etc/resolv.conf"]; ok {
		t.Fatal("overwrites resolver")
	}
}

func TestScopedLinkResources(t *testing.T) {
	f := Files(config.Config{})
	network := string(f["/etc/systemd/network/80-raymond.network"])
	for _, s := range []string{"Address=192.0.2.42/32", "Address=127.0.0.43/32", "DNSDefaultRoute=no"} {
		if !strings.Contains(network, s) {
			t.Fatalf("missing scope setting %s", s)
		}
	}
	link := string(f["/etc/systemd/network/80-raymond.link"])
	for _, s := range []string{"OriginalName=raymond0", "Alias=Raymond private DNS routing"} {
		if !strings.Contains(link, s) {
			t.Fatalf("missing ownership setting %s", s)
		}
	}
	service := string(f["/etc/systemd/system/raymond.service"])
	if !strings.Contains(service, "ExecStart="+BinaryPath+" serve --config /etc/raymond/config.yaml") {
		t.Fatal("service executable path differs from installed artifact")
	}
}
