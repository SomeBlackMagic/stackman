package compose

import "strings"

const hostGatewayToken = "host-gateway"

func HasHostGateway(hosts []string) bool {
	for _, h := range hosts {
		if strings.HasSuffix(h, ":"+hostGatewayToken) {
			return true
		}
	}
	return false
}

func ReplaceHostGatewayToken(hosts []string, gatewayIP string) []string {
	result := make([]string, len(hosts))
	for i, h := range hosts {
		if strings.HasSuffix(h, ":"+hostGatewayToken) {
			result[i] = strings.TrimSuffix(h, ":"+hostGatewayToken) + ":" + gatewayIP
			continue
		}
		result[i] = h
	}
	return result
}
