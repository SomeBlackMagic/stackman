package swarm_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/docker/docker/api/types/network"

	"github.com/SomeBlackMagic/stackman/internal/compose"
	swarmint "github.com/SomeBlackMagic/stackman/internal/swarm"
)

func TestEnsureNetworks_CreatesNew(t *testing.T) {
	t.Parallel()

	createCalled := 0
	mock := &swarmint.MockDockerClient{
		NetworkListFn: func(_ context.Context, _ network.ListOptions) ([]network.Summary, error) {
			return nil, nil
		},
		NetworkCreateFn: func(_ context.Context, name string, opts network.CreateOptions) (network.CreateResponse, error) {
			createCalled++
			if name != "mystack_frontend" {
				t.Errorf("expected network name mystack_frontend, got %q", name)
			}
			if opts.Driver != "overlay" {
				t.Errorf("expected overlay driver, got %q", opts.Driver)
			}
			return network.CreateResponse{ID: "net1"}, nil
		},
	}

	networks := map[string]*compose.Network{
		"frontend": {Driver: "overlay"},
	}

	err := swarmint.EnsureNetworks(context.Background(), mock, "mystack", networks)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if createCalled != 1 {
		t.Errorf("expected 1 NetworkCreate call, got %d", createCalled)
	}
}

func TestEnsureNetworks_SkipsExisting(t *testing.T) {
	t.Parallel()

	createCalled := 0
	mock := &swarmint.MockDockerClient{
		NetworkListFn: func(_ context.Context, _ network.ListOptions) ([]network.Summary, error) {
			return []network.Summary{{Name: "mystack_frontend", ID: "net1"}}, nil
		},
		NetworkCreateFn: func(_ context.Context, _ string, _ network.CreateOptions) (network.CreateResponse, error) {
			createCalled++
			return network.CreateResponse{}, nil
		},
	}

	networks := map[string]*compose.Network{
		"frontend": {Driver: "overlay"},
	}

	err := swarmint.EnsureNetworks(context.Background(), mock, "mystack", networks)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if createCalled != 0 {
		t.Errorf("NetworkCreate should not be called for existing network, got %d calls", createCalled)
	}
}

func TestEnsureNetworks_SkipsExternal(t *testing.T) {
	t.Parallel()

	createCalled := 0
	mock := &swarmint.MockDockerClient{
		NetworkListFn: func(_ context.Context, _ network.ListOptions) ([]network.Summary, error) {
			return nil, nil
		},
		NetworkCreateFn: func(_ context.Context, _ string, _ network.CreateOptions) (network.CreateResponse, error) {
			createCalled++
			return network.CreateResponse{}, nil
		},
	}

	networks := map[string]*compose.Network{
		"external-bool": {External: true},
		"external-map":  {External: map[string]interface{}{"name": "shared-net"}},
	}

	err := swarmint.EnsureNetworks(context.Background(), mock, "mystack", networks)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if createCalled != 0 {
		t.Errorf("external networks should not be created")
	}
}

func TestEnsureNetworks_UsesStackLabelFilter(t *testing.T) {
	t.Parallel()

	mock := &swarmint.MockDockerClient{
		NetworkListFn: func(_ context.Context, opts network.ListOptions) ([]network.Summary, error) {
			labels := opts.Filters.Get("label")
			if len(labels) != 1 {
				t.Fatalf("expected one label filter, got %v", labels)
			}
			if labels[0] != "com.docker.stack.namespace=mystack" {
				t.Fatalf("unexpected label filter: %q", labels[0])
			}
			return nil, nil
		},
		NetworkCreateFn: func(_ context.Context, _ string, _ network.CreateOptions) (network.CreateResponse, error) {
			return network.CreateResponse{}, nil
		},
	}

	err := swarmint.EnsureNetworks(context.Background(), mock, "mystack", map[string]*compose.Network{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestEnsureNetworks_ListError(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("network list failed")
	mock := &swarmint.MockDockerClient{
		NetworkListFn: func(_ context.Context, _ network.ListOptions) ([]network.Summary, error) {
			return nil, wantErr
		},
		NetworkCreateFn: func(_ context.Context, _ string, _ network.CreateOptions) (network.CreateResponse, error) {
			return network.CreateResponse{}, nil
		},
	}

	err := swarmint.EnsureNetworks(context.Background(), mock, "mystack", map[string]*compose.Network{
		"frontend": {Driver: "overlay"},
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected wrapped list error, got %v", err)
	}
	if !strings.Contains(err.Error(), "list networks") {
		t.Fatalf("expected list context in error, got %v", err)
	}
}

func TestEnsureNetworks_CreateError(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("network create failed")
	mock := &swarmint.MockDockerClient{
		NetworkListFn: func(_ context.Context, _ network.ListOptions) ([]network.Summary, error) {
			return nil, nil
		},
		NetworkCreateFn: func(_ context.Context, _ string, _ network.CreateOptions) (network.CreateResponse, error) {
			return network.CreateResponse{}, wantErr
		},
	}

	err := swarmint.EnsureNetworks(context.Background(), mock, "mystack", map[string]*compose.Network{
		"frontend": {Driver: "overlay"},
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected wrapped create error, got %v", err)
	}
	if !strings.Contains(err.Error(), "create network") {
		t.Fatalf("expected create context in error, got %v", err)
	}
}

func TestEnsureNetworks_NilConfigError(t *testing.T) {
	t.Parallel()

	mock := &swarmint.MockDockerClient{
		NetworkListFn: func(_ context.Context, _ network.ListOptions) ([]network.Summary, error) {
			return nil, nil
		},
		NetworkCreateFn: func(_ context.Context, _ string, _ network.CreateOptions) (network.CreateResponse, error) {
			return network.CreateResponse{}, nil
		},
	}

	err := swarmint.EnsureNetworks(context.Background(), mock, "mystack", map[string]*compose.Network{
		"broken": nil,
	})
	if err == nil {
		t.Fatal("expected error for nil network config")
	}
	if !strings.Contains(err.Error(), "convert network \"broken\"") {
		t.Fatalf("expected conversion context in error, got %v", err)
	}
}
