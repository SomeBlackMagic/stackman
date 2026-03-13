package compose

import (
	"fmt"
	"strings"
)

const hostGatewaySpecialValue = "host-gateway"

// ResolveExtraHosts заменяет специальный токен "host-gateway" в записях extra_hosts
// на реальный IP-адрес шлюза Docker-хоста.
// Формат записей: "hostname:host-gateway" → "hostname:<gatewayIP>"
// Если gatewayIP пустой, но запись содержит "host-gateway" — возвращается ошибка.
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
