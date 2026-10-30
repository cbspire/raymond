// Package linux generates isolated systemd-networkd split DNS configuration.
package linux

import (
	"fmt"
	"github.com/cbspire/raymond/internal/config"
	"strings"
)

const BinaryPath = "/usr/libexec/raymond/raymond"

const Marker = "# Raymond owned; do not edit\n"

func Files(c config.Config, management ...bool) map[string][]byte {
	domains := make([]string, len(c.DNS.Zones))
	for i, z := range c.DNS.Zones {
		domains[i] = "~" + z
	}
	credential := ""
	if len(management) > 0 && management[0] {
		credential = "LoadCredential=raymond-control:/var/lib/raymond/management/proxy-token\n"
	}
	return map[string][]byte{
		"/etc/systemd/network/80-raymond.link":    []byte(Marker + "[Match]\nOriginalName=raymond0\n\n[Link]\nAlias=Raymond private DNS routing\n"),
		"/etc/systemd/network/80-raymond.netdev":  []byte(Marker + "[NetDev]\nName=raymond0\nKind=dummy\nDescription=Raymond private DNS routing\n"),
		"/etc/systemd/network/80-raymond.network": []byte(Marker + "[Match]\nName=raymond0\n\n[Link]\nRequiredForOnline=no\n\n[Network]\nAddress=192.0.2.42/32\nAddress=127.0.0.43/32\nLinkLocalAddressing=no\nIPv6AcceptRA=no\nDNS=127.0.0.43\nDomains=" + strings.Join(domains, " ") + "\nDNSDefaultRoute=no\n"),
		"/etc/systemd/system/raymond.service": []byte(Marker + `[Unit]
Description=Raymond local DNS and HTTP
After=network.target systemd-resolved.service systemd-networkd.service
Requires=systemd-resolved.service systemd-networkd.service

[Service]
Type=simple
ExecStart=/usr/libexec/raymond/raymond serve --config /etc/raymond/config.yaml
Restart=on-failure
RestartSec=2
DynamicUser=yes
` + credential + `AmbientCapabilities=CAP_NET_BIND_SERVICE
CapabilityBoundingSet=CAP_NET_BIND_SERVICE
NoNewPrivileges=yes
ProtectSystem=strict
ProtectHome=yes
PrivateTmp=yes
RestrictAddressFamilies=AF_INET AF_UNIX
RestrictNamespaces=yes
LockPersonality=yes
MemoryDenyWriteExecute=yes
SystemCallArchitectures=native
UMask=0077

[Install]
WantedBy=multi-user.target
`),
	}
}
func Check(run func(string, ...string) error) error {
	for _, s := range []string{"systemd-resolved", "systemd-networkd"} {
		if e := run("systemctl", "is-active", "--quiet", s); e != nil {
			return fmt.Errorf("%s must already be active; unsupported resolver/network manager: %w", s, e)
		}
	}
	if e := run("resolvectl", "status"); e != nil {
		return fmt.Errorf("resolvectl unavailable: %w", e)
	}
	return nil
}
