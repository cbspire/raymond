// Package darwin generates launchd and private resolver resources.
package darwin

import (
	"fmt"
	"github.com/cbspire/raymond/internal/config"
	"net"
)

const BinaryPath = "/Library/PrivilegedHelperTools/dev.raymond"
const LogPath = "/var/log/raymond.log"

const Marker = "# Raymond owned; do not edit\n"

func Files(c config.Config) map[string][]byte {
	files := map[string][]byte{"/Library/LaunchDaemons/dev.raymond.plist": []byte(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
<key>Label</key><string>dev.raymond</string>
<key>ProgramArguments</key><array><string>/Library/PrivilegedHelperTools/dev.raymond</string><string>_service</string></array>
<key>RunAtLoad</key><true/><key>KeepAlive</key><true/>
<key>StandardOutPath</key><string>` + LogPath + `</string>
<key>StandardErrorPath</key><string>` + LogPath + `</string>
</dict></plist>
`)}
	address := c.DNS.Listen
	if address == "" {
		address = "127.0.0.43:53"
	}
	host, port, _ := net.SplitHostPort(address)
	for _, z := range c.DNS.Zones {
		files["/etc/resolver/"+z] = []byte(fmt.Sprintf("%snameserver %s\nport %s\n", Marker, host, port))
	}
	return files
}
