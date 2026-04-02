package swarm_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	dockertypes "github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/swarm"

	"github.com/SomeBlackMagic/stackman/internal/compose"
	"github.com/SomeBlackMagic/stackman/internal/deployment"
	swarmint "github.com/SomeBlackMagic/stackman/internal/swarm"
)

func TestDeployServices_CreatesNewService(t *testing.T) {
	t.Parallel()

	createCalled := 0
	var createdSpec swarm.ServiceSpec
	deployID := "deploy-20260101-000000-abc12345"

	mock := &swarmint.MockDockerClient{
		ServiceListFn: func(_ context.Context, _ dockertypes.ServiceListOptions) ([]swarm.Service, error) {
			return nil, nil
		},
		ServiceCreateFn: func(_ context.Context, spec swarm.ServiceSpec, _ dockertypes.ServiceCreateOptions) (swarm.ServiceCreateResponse, error) {
			createCalled++
			createdSpec = spec
			return swarm.ServiceCreateResponse{ID: "svc1"}, nil
		},
		NodeListFn: func(_ context.Context, _ dockertypes.NodeListOptions) ([]swarm.Node, error) {
			return []swarm.Node{{ID: "n1"}}, nil
		},
	}

	services := map[string]*compose.Service{
		"web": {Image: "nginx:latest"},
	}

	results, err := swarmint.DeployServices(context.Background(), mock, "mystack", services, deployID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if createCalled != 1 {
		t.Fatalf("expected 1 ServiceCreate call, got %d", createCalled)
	}
	if len(results) != 1 {
		t.Fatalf("expected one deployment result, got %d", len(results))
	}
	if !results[0].Created {
		t.Fatal("expected Created=true for new service")
	}
	if results[0].ServiceID != "svc1" {
		t.Fatalf("expected result service ID svc1, got %q", results[0].ServiceID)
	}
	if results[0].ServiceName != "mystack_web" {
		t.Fatalf("expected result service name mystack_web, got %q", results[0].ServiceName)
	}
	if got := createdSpec.Annotations.Labels[deployment.DeployIDLabel]; got != deployID {
		t.Fatalf("expected deploy label on service spec %q, got %q", deployID, got)
	}
	if createdSpec.TaskTemplate.ContainerSpec == nil {
		t.Fatal("container spec should not be nil")
	}
	if got := createdSpec.TaskTemplate.ContainerSpec.Labels[deployment.DeployIDLabel]; got != deployID {
		t.Fatalf("expected deploy label on task container spec %q, got %q", deployID, got)
	}
}

func TestDeployServices_UpdatesExisting(t *testing.T) {
	t.Parallel()

	updateCalled := 0
	existingVersion := swarm.Version{Index: 42}
	deployID := "deploy-id"
	var updatedSpec swarm.ServiceSpec

	mock := &swarmint.MockDockerClient{
		ServiceListFn: func(_ context.Context, _ dockertypes.ServiceListOptions) ([]swarm.Service, error) {
			return []swarm.Service{
				{
					ID:   "svc-existing",
					Meta: swarm.Meta{Version: existingVersion},
					Spec: swarm.ServiceSpec{
						Annotations: swarm.Annotations{Name: "mystack_web"},
					},
				},
			}, nil
		},
		ServiceUpdateFn: func(_ context.Context, id string, version swarm.Version, spec swarm.ServiceSpec, _ dockertypes.ServiceUpdateOptions) (swarm.ServiceUpdateResponse, error) {
			updateCalled++
			updatedSpec = spec
			if id != "svc-existing" {
				t.Errorf("expected service id svc-existing, got %q", id)
			}
			if version.Index != 42 {
				t.Errorf("expected service version 42, got %d", version.Index)
			}
			return swarm.ServiceUpdateResponse{}, nil
		},
		NodeListFn: func(_ context.Context, _ dockertypes.NodeListOptions) ([]swarm.Node, error) {
			return []swarm.Node{{ID: "n1"}}, nil
		},
	}

	services := map[string]*compose.Service{
		"web": {Image: "nginx:1.26"},
	}

	results, err := swarmint.DeployServices(context.Background(), mock, "mystack", services, deployID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if updateCalled != 1 {
		t.Fatalf("expected 1 ServiceUpdate call, got %d", updateCalled)
	}
	if len(results) != 1 {
		t.Fatalf("expected one deployment result, got %d", len(results))
	}
	if results[0].Created {
		t.Fatal("expected Created=false for updated service")
	}
	if results[0].ServiceID != "svc-existing" {
		t.Fatalf("expected result service ID svc-existing, got %q", results[0].ServiceID)
	}
	if got := updatedSpec.Annotations.Labels[deployment.DeployIDLabel]; got != deployID {
		t.Fatalf("expected deploy label on updated service spec %q, got %q", deployID, got)
	}
	if updatedSpec.TaskTemplate.ContainerSpec == nil {
		t.Fatal("updated container spec should not be nil")
	}
	if got := updatedSpec.TaskTemplate.ContainerSpec.Labels[deployment.DeployIDLabel]; got != deployID {
		t.Fatalf("expected deploy label on updated task container spec %q, got %q", deployID, got)
	}
}

func TestDeployServices_ServiceListError(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("docker unavailable")
	mock := &swarmint.MockDockerClient{
		ServiceListFn: func(_ context.Context, _ dockertypes.ServiceListOptions) ([]swarm.Service, error) {
			return nil, wantErr
		},
	}

	_, err := swarmint.DeployServices(context.Background(), mock, "mystack", map[string]*compose.Service{}, "deploy-id")
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected wrapped service list error, got %v", err)
	}
	if !strings.Contains(err.Error(), "list existing services") {
		t.Fatalf("expected service list context in error, got %v", err)
	}
}

func TestDeployServices_ServiceCreateError(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("service create failed")

	mock := &swarmint.MockDockerClient{
		ServiceListFn: func(_ context.Context, _ dockertypes.ServiceListOptions) ([]swarm.Service, error) {
			return nil, nil
		},
		ServiceCreateFn: func(_ context.Context, _ swarm.ServiceSpec, _ dockertypes.ServiceCreateOptions) (swarm.ServiceCreateResponse, error) {
			return swarm.ServiceCreateResponse{}, wantErr
		},
		NodeListFn: func(_ context.Context, _ dockertypes.NodeListOptions) ([]swarm.Node, error) {
			return []swarm.Node{{ID: "n1"}}, nil
		},
	}

	_, err := swarmint.DeployServices(context.Background(), mock, "mystack", map[string]*compose.Service{
		"web": {Image: "nginx:latest"},
	}, "deploy-id")
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected wrapped service create error, got %v", err)
	}
	if !strings.Contains(err.Error(), "service create") {
		t.Fatalf("expected service create context in error, got %v", err)
	}
}

func TestDeployServices_ServiceUpdateError(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("update conflict")

	mock := &swarmint.MockDockerClient{
		ServiceListFn: func(_ context.Context, _ dockertypes.ServiceListOptions) ([]swarm.Service, error) {
			return []swarm.Service{
				{
					ID:   "svc-existing",
					Meta: swarm.Meta{Version: swarm.Version{Index: 42}},
					Spec: swarm.ServiceSpec{
						Annotations: swarm.Annotations{Name: "mystack_web"},
					},
				},
			}, nil
		},
		ServiceUpdateFn: func(_ context.Context, _ string, _ swarm.Version, _ swarm.ServiceSpec, _ dockertypes.ServiceUpdateOptions) (swarm.ServiceUpdateResponse, error) {
			return swarm.ServiceUpdateResponse{}, wantErr
		},
		NodeListFn: func(_ context.Context, _ dockertypes.NodeListOptions) ([]swarm.Node, error) {
			return []swarm.Node{{ID: "n1"}}, nil
		},
	}

	_, err := swarmint.DeployServices(context.Background(), mock, "mystack", map[string]*compose.Service{
		"web": {Image: "nginx:latest"},
	}, "deploy-id")
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected wrapped service update error, got %v", err)
	}
	if !strings.Contains(err.Error(), "service update") {
		t.Fatalf("expected service update context in error, got %v", err)
	}
}

func TestDeployServices_IncludesWarningsAndDeployID(t *testing.T) {
	t.Parallel()

	mock := &swarmint.MockDockerClient{
		ServiceListFn: func(_ context.Context, _ dockertypes.ServiceListOptions) ([]swarm.Service, error) {
			return []swarm.Service{
				{
					ID:   "svc-existing",
					Meta: swarm.Meta{Version: swarm.Version{Index: 5}},
					Spec: swarm.ServiceSpec{
						Annotations: swarm.Annotations{Name: "mystack_web"},
					},
				},
			}, nil
		},
		ServiceUpdateFn: func(_ context.Context, _ string, _ swarm.Version, _ swarm.ServiceSpec, _ dockertypes.ServiceUpdateOptions) (swarm.ServiceUpdateResponse, error) {
			return swarm.ServiceUpdateResponse{Warnings: []string{"port collision"}}, nil
		},
		NodeListFn: func(_ context.Context, _ dockertypes.NodeListOptions) ([]swarm.Node, error) {
			return []swarm.Node{{ID: "n1"}}, nil
		},
	}

	results, err := swarmint.DeployServices(context.Background(), mock, "mystack", map[string]*compose.Service{
		"web": {Image: "nginx:latest"},
	}, "deploy-id")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected one result, got %d", len(results))
	}
	if results[0].DeployID != "deploy-id" {
		t.Fatalf("expected deploy-id in result, got %q", results[0].DeployID)
	}
	if len(results[0].Warnings) != 1 || results[0].Warnings[0] != "port collision" {
		t.Fatalf("expected update warnings in result, got %v", results[0].Warnings)
	}
}

func TestDeployServices_MultiNodeKeepsHostGatewayToken(t *testing.T) {
	t.Parallel()

	var createdSpec swarm.ServiceSpec

	mock := &swarmint.MockDockerClient{
		ServiceListFn: func(_ context.Context, _ dockertypes.ServiceListOptions) ([]swarm.Service, error) {
			return nil, nil
		},
		ServiceCreateFn: func(_ context.Context, spec swarm.ServiceSpec, _ dockertypes.ServiceCreateOptions) (swarm.ServiceCreateResponse, error) {
			createdSpec = spec
			return swarm.ServiceCreateResponse{ID: "svc1"}, nil
		},
		NodeListFn: func(_ context.Context, _ dockertypes.NodeListOptions) ([]swarm.Node, error) {
			return []swarm.Node{{ID: "n1"}, {ID: "n2"}}, nil
		},
	}

	_, err := swarmint.DeployServices(context.Background(), mock, "mystack", map[string]*compose.Service{
		"web": {
			Image:      "nginx:latest",
			ExtraHosts: []string{"host.docker.internal:host-gateway"},
		},
	}, "deploy-id")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if createdSpec.TaskTemplate.ContainerSpec == nil {
		t.Fatal("container spec should not be nil")
	}
	gotHosts := createdSpec.TaskTemplate.ContainerSpec.Hosts
	if len(gotHosts) != 1 {
		t.Fatalf("expected one host entry, got %v", gotHosts)
	}
	if gotHosts[0] != "host.docker.internal:host-gateway" {
		t.Fatalf("expected host-gateway token unchanged in multi-node mode, got %q", gotHosts[0])
	}
}
