package linux

import (
	"fmt"
	"github.com/cbspire/raymond/internal/config"
	"strings"
)

// CheckZones refuses overlapping DNS scopes owned by another interface or global
// configuration. A root/default route is ordinary fallback, not a private zone.
func CheckZones(zones []string, output string) error {
	for _, line := range strings.Split(output, "\n") {
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		if strings.Contains(parts[0], "(raymond0)") {
			continue
		}
		for _, field := range strings.Fields(parts[1]) {
			existing, e := config.Normalize(strings.TrimPrefix(field, "~"))
			if e != nil {
				continue
			}
			for _, zone := range zones {
				if config.Within(zone, existing) || config.Within(existing, zone) {
					return fmt.Errorf("DNS zone %s overlaps existing scope %s on %s; refusing to change another resolver's routing", zone, existing, strings.TrimSpace(parts[0]))
				}
			}
		}
	}
	return nil
}
