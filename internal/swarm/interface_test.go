package swarm

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/events"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/api/types/volume"
)

func TestDockerClientInterface(t *testing.T) {
	var _ DockerClient = (*MockDockerClient)(nil)
}

func assertPanicContains(t *testing.T, want string, fn func()) {
	t.Helper()

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic")
		}

		msg := fmt.Sprintf("%v", r)
		if !strings.Contains(msg, want) {
			t.Fatalf("panic message should mention method name, got: %s", msg)
		}
	}()

	fn()
}

func TestMockDockerClient_PanicsWhenFnNotSet(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	cases := []struct {
		name string
		call func(m *MockDockerClient)
	}{
		{
			name: "ServiceList",
			call: func(m *MockDockerClient) {
				_, _ = m.ServiceList(ctx, types.ServiceListOptions{})
			},
		},
		{
			name: "ServiceCreate",
			call: func(m *MockDockerClient) {
				_, _ = m.ServiceCreate(ctx, swarm.ServiceSpec{}, types.ServiceCreateOptions{})
			},
		},
		{
			name: "ServiceUpdate",
			call: func(m *MockDockerClient) {
				_, _ = m.ServiceUpdate(ctx, "svc", swarm.Version{}, swarm.ServiceSpec{}, types.ServiceUpdateOptions{})
			},
		},
		{
			name: "ServiceRemove",
			call: func(m *MockDockerClient) {
				_ = m.ServiceRemove(ctx, "svc")
			},
		},
		{
			name: "ServiceInspectWithRaw",
			call: func(m *MockDockerClient) {
				_, _, _ = m.ServiceInspectWithRaw(ctx, "svc", types.ServiceInspectOptions{})
			},
		},
		{
			name: "TaskList",
			call: func(m *MockDockerClient) {
				_, _ = m.TaskList(ctx, types.TaskListOptions{})
			},
		},
		{
			name: "ContainerList",
			call: func(m *MockDockerClient) {
				_, _ = m.ContainerList(ctx, container.ListOptions{})
			},
		},
		{
			name: "ContainerInspect",
			call: func(m *MockDockerClient) {
				_, _ = m.ContainerInspect(ctx, "ctr")
			},
		},
		{
			name: "ContainerRemove",
			call: func(m *MockDockerClient) {
				_ = m.ContainerRemove(ctx, "ctr", container.RemoveOptions{})
			},
		},
		{
			name: "ContainerLogs",
			call: func(m *MockDockerClient) {
				_, _ = m.ContainerLogs(ctx, "ctr", container.LogsOptions{})
			},
		},
		{
			name: "NetworkCreate",
			call: func(m *MockDockerClient) {
				_, _ = m.NetworkCreate(ctx, "net", network.CreateOptions{})
			},
		},
		{
			name: "NetworkList",
			call: func(m *MockDockerClient) {
				_, _ = m.NetworkList(ctx, network.ListOptions{})
			},
		},
		{
			name: "NetworkRemove",
			call: func(m *MockDockerClient) {
				_ = m.NetworkRemove(ctx, "net")
			},
		},
		{
			name: "VolumeCreate",
			call: func(m *MockDockerClient) {
				_, _ = m.VolumeCreate(ctx, volume.CreateOptions{})
			},
		},
		{
			name: "VolumeList",
			call: func(m *MockDockerClient) {
				_, _ = m.VolumeList(ctx, filters.NewArgs())
			},
		},
		{
			name: "VolumeRemove",
			call: func(m *MockDockerClient) {
				_ = m.VolumeRemove(ctx, "vol", true)
			},
		},
		{
			name: "Events",
			call: func(m *MockDockerClient) {
				_, _ = m.Events(ctx, events.ListOptions{})
			},
		},
		{
			name: "NodeList",
			call: func(m *MockDockerClient) {
				_, _ = m.NodeList(ctx, types.NodeListOptions{})
			},
		},
		{
			name: "ImagePull",
			call: func(m *MockDockerClient) {
				_, _ = m.ImagePull(ctx, "alpine:latest", image.PullOptions{})
			},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			mock := &MockDockerClient{}
			assertPanicContains(t, tc.name, func() {
				tc.call(mock)
			})
		})
	}
}

func TestMockDockerClient_CloseWithoutFnReturnsNil(t *testing.T) {
	t.Parallel()

	mock := &MockDockerClient{}
	if err := mock.Close(); err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}
}

func TestMockDockerClient_CloseDelegatesToFn(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("close failed")
	called := false

	mock := &MockDockerClient{
		CloseFn: func() error {
			called = true
			return wantErr
		},
	}

	err := mock.Close()
	if !called {
		t.Fatal("CloseFn was not called")
	}
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected error %v, got %v", wantErr, err)
	}
}

type stubReadCloser struct{}

func (s *stubReadCloser) Read(_ []byte) (int, error) {
	return 0, io.EOF
}

func (s *stubReadCloser) Close() error {
	return nil
}

func TestMockDockerClient_DelegatesToConfiguredFns(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	t.Run("ServiceList", func(t *testing.T) {
		t.Parallel()
		want := []swarm.Service{{ID: "svc1"}}
		mock := &MockDockerClient{
			ServiceListFn: func(_ context.Context, _ types.ServiceListOptions) ([]swarm.Service, error) {
				return want, nil
			},
		}
		got, err := mock.ServiceList(ctx, types.ServiceListOptions{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(got) != 1 || got[0].ID != "svc1" {
			t.Fatalf("unexpected services: %+v", got)
		}
	})

	t.Run("ServiceCreate", func(t *testing.T) {
		t.Parallel()
		called := false
		mock := &MockDockerClient{
			ServiceCreateFn: func(_ context.Context, service swarm.ServiceSpec, _ types.ServiceCreateOptions) (swarm.ServiceCreateResponse, error) {
				called = true
				if service.Annotations.Name != "api" {
					t.Fatalf("unexpected service name: %s", service.Annotations.Name)
				}
				return swarm.ServiceCreateResponse{ID: "svc-created"}, nil
			},
		}
		got, err := mock.ServiceCreate(ctx, swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "api"}}, types.ServiceCreateOptions{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !called || got.ID != "svc-created" {
			t.Fatalf("unexpected response: %+v (called=%v)", got, called)
		}
	})

	t.Run("ServiceUpdate", func(t *testing.T) {
		t.Parallel()
		called := false
		mock := &MockDockerClient{
			ServiceUpdateFn: func(_ context.Context, serviceID string, version swarm.Version, _ swarm.ServiceSpec, _ types.ServiceUpdateOptions) (swarm.ServiceUpdateResponse, error) {
				called = true
				if serviceID != "svc1" {
					t.Fatalf("unexpected service id: %s", serviceID)
				}
				if version.Index != 42 {
					t.Fatalf("unexpected version index: %d", version.Index)
				}
				return swarm.ServiceUpdateResponse{Warnings: []string{"warn"}}, nil
			},
		}
		got, err := mock.ServiceUpdate(ctx, "svc1", swarm.Version{Index: 42}, swarm.ServiceSpec{}, types.ServiceUpdateOptions{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !called || len(got.Warnings) != 1 || got.Warnings[0] != "warn" {
			t.Fatalf("unexpected response: %+v (called=%v)", got, called)
		}
	})

	t.Run("ServiceRemove", func(t *testing.T) {
		t.Parallel()
		called := false
		mock := &MockDockerClient{
			ServiceRemoveFn: func(_ context.Context, serviceID string) error {
				called = true
				if serviceID != "svc1" {
					t.Fatalf("unexpected service id: %s", serviceID)
				}
				return nil
			},
		}
		if err := mock.ServiceRemove(ctx, "svc1"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !called {
			t.Fatal("ServiceRemoveFn was not called")
		}
	})

	t.Run("ServiceInspectWithRaw", func(t *testing.T) {
		t.Parallel()
		wantRaw := []byte("raw")
		mock := &MockDockerClient{
			ServiceInspectWithRawFn: func(_ context.Context, serviceID string, _ types.ServiceInspectOptions) (swarm.Service, []byte, error) {
				if serviceID != "svc1" {
					t.Fatalf("unexpected service id: %s", serviceID)
				}
				return swarm.Service{ID: "svc1"}, wantRaw, nil
			},
		}
		gotSvc, gotRaw, err := mock.ServiceInspectWithRaw(ctx, "svc1", types.ServiceInspectOptions{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if gotSvc.ID != "svc1" || string(gotRaw) != "raw" {
			t.Fatalf("unexpected result: svc=%+v raw=%q", gotSvc, string(gotRaw))
		}
	})

	t.Run("TaskList", func(t *testing.T) {
		t.Parallel()
		mock := &MockDockerClient{
			TaskListFn: func(_ context.Context, _ types.TaskListOptions) ([]swarm.Task, error) {
				return []swarm.Task{{ID: "task1"}}, nil
			},
		}
		got, err := mock.TaskList(ctx, types.TaskListOptions{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(got) != 1 || got[0].ID != "task1" {
			t.Fatalf("unexpected tasks: %+v", got)
		}
	})

	t.Run("ContainerList", func(t *testing.T) {
		t.Parallel()
		mock := &MockDockerClient{
			ContainerListFn: func(_ context.Context, opts container.ListOptions) ([]types.Container, error) {
				if !opts.All {
					t.Fatal("expected All=true")
				}
				return []types.Container{{ID: "ctr1"}}, nil
			},
		}
		got, err := mock.ContainerList(ctx, container.ListOptions{All: true})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(got) != 1 || got[0].ID != "ctr1" {
			t.Fatalf("unexpected containers: %+v", got)
		}
	})

	t.Run("ContainerInspect", func(t *testing.T) {
		t.Parallel()
		mock := &MockDockerClient{
			ContainerInspectFn: func(_ context.Context, containerID string) (types.ContainerJSON, error) {
				if containerID != "ctr1" {
					t.Fatalf("unexpected container id: %s", containerID)
				}
				return types.ContainerJSON{ContainerJSONBase: &types.ContainerJSONBase{ID: "ctr1"}}, nil
			},
		}
		got, err := mock.ContainerInspect(ctx, "ctr1")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.ContainerJSONBase == nil || got.ContainerJSONBase.ID != "ctr1" {
			t.Fatalf("unexpected container inspect result: %+v", got)
		}
	})

	t.Run("ContainerRemove", func(t *testing.T) {
		t.Parallel()
		mock := &MockDockerClient{
			ContainerRemoveFn: func(_ context.Context, containerID string, opts container.RemoveOptions) error {
				if containerID != "ctr1" {
					t.Fatalf("unexpected container id: %s", containerID)
				}
				if !opts.Force {
					t.Fatal("expected Force=true")
				}
				return nil
			},
		}
		if err := mock.ContainerRemove(ctx, "ctr1", container.RemoveOptions{Force: true}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("ContainerLogs", func(t *testing.T) {
		t.Parallel()
		wantReader := &stubReadCloser{}
		mock := &MockDockerClient{
			ContainerLogsFn: func(_ context.Context, containerID string, opts container.LogsOptions) (io.ReadCloser, error) {
				if containerID != "ctr1" {
					t.Fatalf("unexpected container id: %s", containerID)
				}
				if !opts.ShowStdout {
					t.Fatal("expected ShowStdout=true")
				}
				return wantReader, nil
			},
		}
		got, err := mock.ContainerLogs(ctx, "ctr1", container.LogsOptions{ShowStdout: true})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != wantReader {
			t.Fatalf("unexpected reader: %v", got)
		}
	})

	t.Run("NetworkCreate", func(t *testing.T) {
		t.Parallel()
		mock := &MockDockerClient{
			NetworkCreateFn: func(_ context.Context, name string, _ network.CreateOptions) (network.CreateResponse, error) {
				if name != "net1" {
					t.Fatalf("unexpected network name: %s", name)
				}
				return network.CreateResponse{ID: "net-id"}, nil
			},
		}
		got, err := mock.NetworkCreate(ctx, "net1", network.CreateOptions{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.ID != "net-id" {
			t.Fatalf("unexpected network create response: %+v", got)
		}
	})

	t.Run("NetworkList", func(t *testing.T) {
		t.Parallel()
		mock := &MockDockerClient{
			NetworkListFn: func(_ context.Context, _ network.ListOptions) ([]network.Summary, error) {
				return []network.Summary{{ID: "net1"}}, nil
			},
		}
		got, err := mock.NetworkList(ctx, network.ListOptions{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(got) != 1 || got[0].ID != "net1" {
			t.Fatalf("unexpected networks: %+v", got)
		}
	})

	t.Run("NetworkRemove", func(t *testing.T) {
		t.Parallel()
		mock := &MockDockerClient{
			NetworkRemoveFn: func(_ context.Context, networkID string) error {
				if networkID != "net1" {
					t.Fatalf("unexpected network id: %s", networkID)
				}
				return nil
			},
		}
		if err := mock.NetworkRemove(ctx, "net1"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("VolumeCreate", func(t *testing.T) {
		t.Parallel()
		mock := &MockDockerClient{
			VolumeCreateFn: func(_ context.Context, opts volume.CreateOptions) (volume.Volume, error) {
				if opts.Name != "vol1" {
					t.Fatalf("unexpected volume name: %s", opts.Name)
				}
				return volume.Volume{Name: "vol1"}, nil
			},
		}
		got, err := mock.VolumeCreate(ctx, volume.CreateOptions{Name: "vol1"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.Name != "vol1" {
			t.Fatalf("unexpected volume: %+v", got)
		}
	})

	t.Run("VolumeList", func(t *testing.T) {
		t.Parallel()
		mock := &MockDockerClient{
			VolumeListFn: func(_ context.Context, filter filters.Args) (volume.ListResponse, error) {
				if !filter.ExactMatch("label", "stack=demo") {
					t.Fatalf("expected label filter stack=demo")
				}
				return volume.ListResponse{
					Volumes: []*volume.Volume{{Name: "vol1"}},
				}, nil
			},
		}
		args := filters.NewArgs()
		args.Add("label", "stack=demo")
		got, err := mock.VolumeList(ctx, args)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(got.Volumes) != 1 || got.Volumes[0].Name != "vol1" {
			t.Fatalf("unexpected volume list result: %+v", got)
		}
	})

	t.Run("VolumeRemove", func(t *testing.T) {
		t.Parallel()
		mock := &MockDockerClient{
			VolumeRemoveFn: func(_ context.Context, volumeID string, force bool) error {
				if volumeID != "vol1" {
					t.Fatalf("unexpected volume id: %s", volumeID)
				}
				if !force {
					t.Fatal("expected force=true")
				}
				return nil
			},
		}
		if err := mock.VolumeRemove(ctx, "vol1", true); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("Events", func(t *testing.T) {
		t.Parallel()
		wantEvents := make(chan events.Message, 1)
		wantErrs := make(chan error, 1)
		mock := &MockDockerClient{
			EventsFn: func(_ context.Context, opts events.ListOptions) (<-chan events.Message, <-chan error) {
				if opts.Since != "10" {
					t.Fatalf("unexpected since value: %s", opts.Since)
				}
				return wantEvents, wantErrs
			},
		}
		gotEvents, gotErrs := mock.Events(ctx, events.ListOptions{Since: "10"})
		if gotEvents != wantEvents {
			t.Fatalf("unexpected events channel")
		}
		if gotErrs != wantErrs {
			t.Fatalf("unexpected error channel")
		}
	})

	t.Run("NodeList", func(t *testing.T) {
		t.Parallel()
		mock := &MockDockerClient{
			NodeListFn: func(_ context.Context, _ types.NodeListOptions) ([]swarm.Node, error) {
				return []swarm.Node{{ID: "node1"}}, nil
			},
		}
		got, err := mock.NodeList(ctx, types.NodeListOptions{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(got) != 1 || got[0].ID != "node1" {
			t.Fatalf("unexpected nodes: %+v", got)
		}
	})

	t.Run("ImagePull", func(t *testing.T) {
		t.Parallel()
		wantReader := &stubReadCloser{}
		mock := &MockDockerClient{
			ImagePullFn: func(_ context.Context, refStr string, _ image.PullOptions) (io.ReadCloser, error) {
				if refStr != "alpine:latest" {
					t.Fatalf("unexpected image ref: %s", refStr)
				}
				return wantReader, nil
			},
		}
		got, err := mock.ImagePull(ctx, "alpine:latest", image.PullOptions{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != wantReader {
			t.Fatalf("unexpected image pull reader: %v", got)
		}
	})
}

func TestNewEmptyStackMock_Defaults(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	mock := NewEmptyStackMock()

	services, err := mock.ServiceList(ctx, types.ServiceListOptions{})
	if err != nil {
		t.Fatalf("unexpected service list error: %v", err)
	}
	if len(services) != 0 {
		t.Fatalf("expected empty service list, got: %+v", services)
	}

	networks, err := mock.NetworkList(ctx, network.ListOptions{})
	if err != nil {
		t.Fatalf("unexpected network list error: %v", err)
	}
	if len(networks) != 0 {
		t.Fatalf("expected empty network list, got: %+v", networks)
	}

	volumesResp, err := mock.VolumeList(ctx, filters.NewArgs())
	if err != nil {
		t.Fatalf("unexpected volume list error: %v", err)
	}
	if len(volumesResp.Volumes) != 0 {
		t.Fatalf("expected empty volume list, got: %+v", volumesResp)
	}

	nodes, err := mock.NodeList(ctx, types.NodeListOptions{})
	if err != nil {
		t.Fatalf("unexpected node list error: %v", err)
	}
	if len(nodes) != 1 || nodes[0].ID != "node1" {
		t.Fatalf("unexpected nodes: %+v", nodes)
	}

	assertPanicContains(t, "ContainerList", func() {
		_, _ = mock.ContainerList(ctx, container.ListOptions{})
	})
}
