package compose_test

import (
	"reflect"
	"testing"

	"github.com/SomeBlackMagic/stackman/internal/compose"
)

func TestReplaceHostGatewayToken(t *testing.T) {
	tests := []struct {
		input   []string
		gateway string
		want    []string
	}{
		{
			input:   []string{"host.docker.internal:host-gateway"},
			gateway: "172.17.0.1",
			want:    []string{"host.docker.internal:172.17.0.1"},
		},
		{
			input:   []string{"db:192.168.1.1"},
			gateway: "172.17.0.1",
			want:    []string{"db:192.168.1.1"},
		},
		{
			input:   []string{},
			gateway: "172.17.0.1",
			want:    []string{},
		},
	}

	for _, tt := range tests {
		got := compose.ReplaceHostGatewayToken(tt.input, tt.gateway)
		if !reflect.DeepEqual(got, tt.want) {
			t.Errorf("input=%v gateway=%v: got %v, want %v", tt.input, tt.gateway, got, tt.want)
		}
	}
}

func TestHasHostGateway(t *testing.T) {
	if !compose.HasHostGateway([]string{"host.docker.internal:host-gateway"}) {
		t.Error("expected true")
	}
	if compose.HasHostGateway([]string{"db:192.168.1.1"}) {
		t.Error("expected false")
	}
}
