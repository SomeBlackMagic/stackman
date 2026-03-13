package compose

import (
	"fmt"
	"strings"
)

const hostGatewaySpecialValue = "host-gateway"

// ResolveExtraHosts replaces the special "host-gateway" token in extra_hosts entries
// with the actual Docker host gateway IP address.
// Entry format: "hostname:host-gateway" -> "hostname:<gatewayIP>"
// Returns an error if gatewayIP is empty but an entry contains "host-gateway".
func ResolveExtraHosts(hosts []string, gatewayIP string) ([]string, error) {
	resolved := make([]string, 0, len(hosts))
	for _, entry := range hosts {
		if strings.Contains(entry, hostGatewaySpecialValue) {
			if gatewayIP == "" {
				return nil, fmt.Errorf("extra_hosts: cannot resolve %q: gateway IP is empty", entry)
			}
			resolved = append(resolved, strings.ReplaceAll(entry, hostGatewaySpecialValue, gatewayIP))
		} else {
			resolved = append(resolved, entry)
		}
	}
	return resolved, nil
}
