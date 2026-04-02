package swarm_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/volume"

	"github.com/SomeBlackMagic/stackman/internal/compose"
	swarmint "github.com/SomeBlackMagic/stackman/internal/swarm"
)

func TestEnsureVolumes_CreatesNew(t *testing.T) {
	t.Parallel()

	createCalled := 0
	mock := &swarmint.MockDockerClient{
		VolumeListFn: func(_ context.Context, _ filters.Args) (volume.ListResponse, error) {
			return volume.ListResponse{}, nil
		},
		VolumeCreateFn: func(_ context.Context, opts volume.CreateOptions) (volume.Volume, error) {
			createCalled++
			if opts.Name != "mystack_data" {
				t.Errorf("expected volume name mystack_data, got %q", opts.Name)
			}
			if opts.Driver != "local" {
				t.Errorf("expected local driver, got %q", opts.Driver)
			}
			return volume.Volume{Name: opts.Name}, nil
		},
	}

	volumes := map[string]*compose.Volume{
		"data": {Driver: "local"},
	}

	err := swarmint.EnsureVolumes(context.Background(), mock, "mystack", volumes)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if createCalled != 1 {
		t.Errorf("expected 1 VolumeCreate call, got %d", createCalled)
	}
}

func TestEnsureVolumes_SkipsExisting(t *testing.T) {
	t.Parallel()

	createCalled := 0
	mock := &swarmint.MockDockerClient{
		VolumeListFn: func(_ context.Context, _ filters.Args) (volume.ListResponse, error) {
			return volume.ListResponse{
				Volumes: []*volume.Volume{{Name: "mystack_data"}},
			}, nil
		},
		VolumeCreateFn: func(_ context.Context, _ volume.CreateOptions) (volume.Volume, error) {
			createCalled++
			return volume.Volume{}, nil
		},
	}

	volumes := map[string]*compose.Volume{
		"data": {},
	}

	err := swarmint.EnsureVolumes(context.Background(), mock, "mystack", volumes)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if createCalled != 0 {
		t.Errorf("VolumeCreate should not be called for existing volume")
	}
}

func TestEnsureVolumes_SkipsExternal(t *testing.T) {
	t.Parallel()

	createCalled := 0
	mock := &swarmint.MockDockerClient{
		VolumeListFn: func(_ context.Context, _ filters.Args) (volume.ListResponse, error) {
			return volume.ListResponse{}, nil
		},
		VolumeCreateFn: func(_ context.Context, _ volume.CreateOptions) (volume.Volume, error) {
			createCalled++
			return volume.Volume{}, nil
		},
	}

	volumes := map[string]*compose.Volume{
		"external-bool": {External: true},
		"external-map":  {External: map[string]interface{}{"name": "shared-vol"}},
	}

	err := swarmint.EnsureVolumes(context.Background(), mock, "mystack", volumes)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if createCalled != 0 {
		t.Errorf("external volumes should not be created")
	}
}

func TestEnsureVolumes_UsesStackLabelFilter(t *testing.T) {
	t.Parallel()

	mock := &swarmint.MockDockerClient{
		VolumeListFn: func(_ context.Context, args filters.Args) (volume.ListResponse, error) {
			labels := args.Get("label")
			if len(labels) != 1 {
				t.Fatalf("expected one label filter, got %v", labels)
			}
			if labels[0] != "com.docker.stack.namespace=mystack" {
				t.Fatalf("unexpected label filter: %q", labels[0])
			}
			return volume.ListResponse{}, nil
		},
		VolumeCreateFn: func(_ context.Context, _ volume.CreateOptions) (volume.Volume, error) {
			return volume.Volume{}, nil
		},
	}

	err := swarmint.EnsureVolumes(context.Background(), mock, "mystack", map[string]*compose.Volume{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestEnsureVolumes_ListError(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("volume list failed")
	mock := &swarmint.MockDockerClient{
		VolumeListFn: func(_ context.Context, _ filters.Args) (volume.ListResponse, error) {
			return volume.ListResponse{}, wantErr
		},
		VolumeCreateFn: func(_ context.Context, _ volume.CreateOptions) (volume.Volume, error) {
			return volume.Volume{}, nil
		},
	}

	err := swarmint.EnsureVolumes(context.Background(), mock, "mystack", map[string]*compose.Volume{
		"data": {Driver: "local"},
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected wrapped list error, got %v", err)
	}
	if !strings.Contains(err.Error(), "list volumes") {
		t.Fatalf("expected list context in error, got %v", err)
	}
}

func TestEnsureVolumes_CreateError(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("volume create failed")
	mock := &swarmint.MockDockerClient{
		VolumeListFn: func(_ context.Context, _ filters.Args) (volume.ListResponse, error) {
			return volume.ListResponse{}, nil
		},
		VolumeCreateFn: func(_ context.Context, _ volume.CreateOptions) (volume.Volume, error) {
			return volume.Volume{}, wantErr
		},
	}

	err := swarmint.EnsureVolumes(context.Background(), mock, "mystack", map[string]*compose.Volume{
		"data": {Driver: "local"},
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected wrapped create error, got %v", err)
	}
	if !strings.Contains(err.Error(), "create volume") {
		t.Fatalf("expected create context in error, got %v", err)
	}
}

func TestEnsureVolumes_NilConfigError(t *testing.T) {
	t.Parallel()

	mock := &swarmint.MockDockerClient{
		VolumeListFn: func(_ context.Context, _ filters.Args) (volume.ListResponse, error) {
			return volume.ListResponse{}, nil
		},
		VolumeCreateFn: func(_ context.Context, _ volume.CreateOptions) (volume.Volume, error) {
			return volume.Volume{}, nil
		},
	}

	err := swarmint.EnsureVolumes(context.Background(), mock, "mystack", map[string]*compose.Volume{
		"broken": nil,
	})
	if err == nil {
		t.Fatal("expected error for nil volume config")
	}
	if !strings.Contains(err.Error(), "convert volume \"broken\"") {
		t.Fatalf("expected conversion context in error, got %v", err)
	}
}
