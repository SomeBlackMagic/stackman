package compose

import (
	"testing"
)

func TestResolveExtraHosts(t *testing.T) {
	tests := []struct {
		name        string
		hosts       []string
		gatewayIP   string
		expected    []string
		expectError bool
	}{
		{
			name:      "single host-gateway entry is resolved",
			hosts:     []string{"host.docker.internal:host-gateway"},
			gatewayIP: "172.17.0.1",
			expected:  []string{"host.docker.internal:172.17.0.1"},
		},
		{
			name:      "regular entry is not changed",
			hosts:     []string{"db:5.5.5.5"},
			gatewayIP: "172.17.0.1",
			expected:  []string{"db:5.5.5.5"},
		},
		{
			name:      "mixed list: only host-gateway entry is replaced",
			hosts:     []string{"db:5.5.5.5", "host.docker.internal:host-gateway"},
			gatewayIP: "10.0.0.1",
			expected:  []string{"db:5.5.5.5", "host.docker.internal:10.0.0.1"},
		},
		{
			name:      "multiple host-gateway entries are all resolved",
			hosts:     []string{"foo:host-gateway", "bar:host-gateway"},
			gatewayIP: "192.168.1.1",
			expected:  []string{"foo:192.168.1.1", "bar:192.168.1.1"},
		},
		{
			name:        "empty gatewayIP with host-gateway entry returns error",
			hosts:       []string{"host.docker.internal:host-gateway"},
			gatewayIP:   "",
			expectError: true,
		},
		{
			name:      "empty hosts list returns empty list without error",
			hosts:     []string{},
			gatewayIP: "172.17.0.1",
			expected:  []string{},
		},
		{
			name:      "nil hosts list returns empty list without error",
			hosts:     nil,
			gatewayIP: "172.17.0.1",
			expected:  []string{},
		},
		{
			name:      "empty gatewayIP with no host-gateway entries returns no error",
			hosts:     []string{"db:1.2.3.4"},
			gatewayIP: "",
			expected:  []string{"db:1.2.3.4"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ResolveExtraHosts(tt.hosts, tt.gatewayIP)

			if tt.expectError {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if len(result) != len(tt.expected) {
				t.Fatalf("expected %d entries, got %d: %v", len(tt.expected), len(result), result)
			}

			for i, entry := range result {
				if entry != tt.expected[i] {
					t.Errorf("entry[%d]: expected %q, got %q", i, tt.expected[i], entry)
				}
			}
		})
	}
}
