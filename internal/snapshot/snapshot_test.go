package snapshot_test

import (
	"context"
	"errors"
	"testing"

	dockertypes "github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/filters"
	dockernetwork "github.com/docker/docker/api/types/network"
	dockerswarm "github.com/docker/docker/api/types/swarm"
	dockervolume "github.com/docker/docker/api/types/volume"

	snapmod "github.com/SomeBlackMagic/stackman/internal/snapshot"
	swarmint "github.com/SomeBlackMagic/stackman/internal/swarm"
)

func newStateMock(services []dockerswarm.Service, networks []dockernetwork.Summary, volumes []dockervolume.Volume) *swarmint.MockDockerClient {
	return &swarmint.MockDockerClient{
		ServiceListFn: func(_ context.Context, _ dockertypes.ServiceListOptions) ([]dockerswarm.Service, error) {
			return services, nil
		},
		NetworkListFn: func(_ context.Context, _ dockernetwork.ListOptions) ([]dockernetwork.Summary, error) {
			return networks, nil
		},
		VolumeListFn: func(_ context.Context, _ filters.Args) (dockervolume.ListResponse, error) {
			items := make([]*dockervolume.Volume, 0, len(volumes))
			for i := range volumes {
				items = append(items, &volumes[i])
			}
			return dockervolume.ListResponse{Volumes: items}, nil
		},
	}
}

func emptySnapshot(stackName string) *snapmod.StackSnapshot {
	return &snapmod.StackSnapshot{
		StackName: stackName,
		Services:  map[string]dockerswarm.Service{},
		Networks:  map[string]dockernetwork.Summary{},
		Volumes:   map[string]dockervolume.Volume{},
	}
}

func TestTakeSnapshot_PositiveCaseEmptyStack(t *testing.T) {
	t.Parallel()

	deployer := swarmint.NewStackDeployer(newStateMock(nil, nil, nil), "mystack")
	snap, err := snapmod.TakeSnapshot(context.Background(), deployer)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if snap.StackName != "mystack" {
		t.Fatalf("expected stack name mystack, got %q", snap.StackName)
	}
	if !snap.IsFirstDeploy {
		t.Fatal("expected IsFirstDeploy=true for empty stack")
	}
	if snap.TakenAt.IsZero() {
		t.Fatal("expected non-zero TakenAt")
	}
	if len(snap.Services) != 0 || len(snap.Networks) != 0 || len(snap.Volumes) != 0 {
		t.Fatalf("expected empty maps, got services=%d networks=%d volumes=%d", len(snap.Services), len(snap.Networks), len(snap.Volumes))
	}
}

func TestTakeSnapshot_PositiveCaseCapturesResources(t *testing.T) {
	t.Parallel()

	services := []dockerswarm.Service{
		{
			ID: "svc-web",
			Spec: dockerswarm.ServiceSpec{
				Annotations: dockerswarm.Annotations{Name: "mystack_web"},
			},
		},
	}
	networks := []dockernetwork.Summary{
		{ID: "net-default", Name: "mystack_default", Driver: "overlay"},
	}
	volumes := []dockervolume.Volume{
		{Name: "mystack_data", Driver: "local"},
	}

	deployer := swarmint.NewStackDeployer(newStateMock(services, networks, volumes), "mystack")
	snap, err := snapmod.TakeSnapshot(context.Background(), deployer)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if snap.IsFirstDeploy {
		t.Fatal("expected IsFirstDeploy=false when services exist")
	}
	if _, ok := snap.Services["web"]; !ok {
		t.Fatalf("expected service key web, got %v", snap.Services)
	}
	if _, ok := snap.Networks["default"]; !ok {
		t.Fatalf("expected network key default, got %v", snap.Networks)
	}
	if _, ok := snap.Volumes["data"]; !ok {
		t.Fatalf("expected volume key data, got %v", snap.Volumes)
	}
}

func TestTakeSnapshot_NegativeCaseNilDeployer(t *testing.T) {
	t.Parallel()

	_, err := snapmod.TakeSnapshot(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error for nil deployer")
	}
}

func TestTakeSnapshot_NegativeCaseGetStateError(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("service list failed")
	mock := &swarmint.MockDockerClient{
		ServiceListFn: func(_ context.Context, _ dockertypes.ServiceListOptions) ([]dockerswarm.Service, error) {
			return nil, wantErr
		},
		NetworkListFn: func(_ context.Context, _ dockernetwork.ListOptions) ([]dockernetwork.Summary, error) {
			return nil, nil
		},
		VolumeListFn: func(_ context.Context, _ filters.Args) (dockervolume.ListResponse, error) {
			return dockervolume.ListResponse{}, nil
		},
	}

	deployer := swarmint.NewStackDeployer(mock, "mystack")
	_, err := snapmod.TakeSnapshot(context.Background(), deployer)
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected wrapped error %v, got %v", wantErr, err)
	}
}

func TestRollback_PositiveCaseFirstDeployRemovesAllResources(t *testing.T) {
	t.Parallel()

	currentServices := []dockerswarm.Service{
		{
			ID: "svc-web",
			Spec: dockerswarm.ServiceSpec{
				Annotations: dockerswarm.Annotations{Name: "mystack_web"},
			},
		},
		{
			ID: "svc-worker",
			Spec: dockerswarm.ServiceSpec{
				Annotations: dockerswarm.Annotations{Name: "mystack_worker"},
			},
		},
	}
	currentNetworks := []dockernetwork.Summary{
		{ID: "net-default", Name: "mystack_default"},
	}
	currentVolumes := []dockervolume.Volume{
		{Name: "mystack_data", Driver: "local"},
	}

	removedServices := map[string]bool{}
	removedNetworks := map[string]bool{}
	removedVolumes := map[string]bool{}

	mock := newStateMock(currentServices, currentNetworks, currentVolumes)
	mock.ServiceRemoveFn = func(_ context.Context, serviceID string) error {
		removedServices[serviceID] = true
		return nil
	}
	mock.NetworkRemoveFn = func(_ context.Context, networkID string) error {
		removedNetworks[networkID] = true
		return nil
	}
	mock.VolumeRemoveFn = func(_ context.Context, volumeID string, force bool) error {
		if !force {
			t.Fatal("expected force=true for volume rollback removal")
		}
		removedVolumes[volumeID] = true
		return nil
	}

	snap := emptySnapshot("mystack")
	snap.IsFirstDeploy = true

	err := snap.Rollback(context.Background(), mock)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !removedServices["svc-web"] || !removedServices["svc-worker"] {
		t.Fatalf("expected all services to be removed, got %v", removedServices)
	}
	if !removedNetworks["net-default"] {
		t.Fatalf("expected default network to be removed, got %v", removedNetworks)
	}
	if !removedVolumes["mystack_data"] {
		t.Fatalf("expected data volume to be removed, got %v", removedVolumes)
	}
}

func TestRollback_PositiveCaseUpdateRestoresAndSyncsAllResources(t *testing.T) {
	t.Parallel()

	currentServices := []dockerswarm.Service{
		{
			ID:   "svc-web",
			Meta: dockerswarm.Meta{Version: dockerswarm.Version{Index: 9}},
			Spec: dockerswarm.ServiceSpec{
				Annotations: dockerswarm.Annotations{Name: "mystack_web"},
				TaskTemplate: dockerswarm.TaskSpec{
					ContainerSpec: &dockerswarm.ContainerSpec{Image: "nginx:1.25"},
				},
			},
		},
		{
			ID:   "svc-worker",
			Meta: dockerswarm.Meta{Version: dockerswarm.Version{Index: 4}},
			Spec: dockerswarm.ServiceSpec{
				Annotations: dockerswarm.Annotations{Name: "mystack_worker"},
			},
		},
	}
	currentNetworks := []dockernetwork.Summary{
		{ID: "net-default", Name: "mystack_default", Driver: "overlay"},
		{ID: "net-metrics", Name: "mystack_metrics", Driver: "overlay"},
	}
	currentVolumes := []dockervolume.Volume{
		{Name: "mystack_data", Driver: "local"},
		{Name: "mystack_cache", Driver: "local"},
	}

	oldWebSpec := dockerswarm.ServiceSpec{
		Annotations: dockerswarm.Annotations{Name: "mystack_web"},
		TaskTemplate: dockerswarm.TaskSpec{
			ContainerSpec: &dockerswarm.ContainerSpec{Image: "nginx:1.24"},
		},
	}
	apiSpec := dockerswarm.ServiceSpec{
		Annotations: dockerswarm.Annotations{Name: "mystack_api"},
		TaskTemplate: dockerswarm.TaskSpec{
			ContainerSpec: &dockerswarm.ContainerSpec{Image: "nginx:1.24"},
		},
	}

	snapshotNetworks := map[string]dockernetwork.Summary{
		"default": {Name: "mystack_default", Driver: "overlay"},
		"backend": {
			Name:       "mystack_backend",
			Driver:     "overlay",
			Attachable: true,
			Internal:   true,
			Options: map[string]string{
				"encrypted": "true",
			},
			Labels: map[string]string{
				"com.docker.stack.namespace": "mystack",
			},
		},
	}
	snapshotVolumes := map[string]dockervolume.Volume{
		"data": {Name: "mystack_data", Driver: "local"},
		"logs": {
			Name:   "mystack_logs",
			Driver: "local",
			Options: map[string]string{
				"type": "tmpfs",
			},
			Labels: map[string]string{
				"com.docker.stack.namespace": "mystack",
			},
		},
	}

	removedServices := map[string]bool{}
	updatedServices := map[string]dockerswarm.ServiceSpec{}
	createdServices := map[string]bool{}

	removedNetworks := map[string]bool{}
	createdNetworks := map[string]dockernetwork.CreateOptions{}

	removedVolumes := map[string]bool{}
	createdVolumes := map[string]dockervolume.CreateOptions{}

	mock := newStateMock(currentServices, currentNetworks, currentVolumes)
	mock.ServiceRemoveFn = func(_ context.Context, serviceID string) error {
		removedServices[serviceID] = true
		return nil
	}
	mock.ServiceUpdateFn = func(_ context.Context, serviceID string, _ dockerswarm.Version, spec dockerswarm.ServiceSpec, _ dockertypes.ServiceUpdateOptions) (dockerswarm.ServiceUpdateResponse, error) {
		updatedServices[serviceID] = spec
		return dockerswarm.ServiceUpdateResponse{}, nil
	}
	mock.ServiceCreateFn = func(_ context.Context, spec dockerswarm.ServiceSpec, _ dockertypes.ServiceCreateOptions) (dockerswarm.ServiceCreateResponse, error) {
		createdServices[spec.Name] = true
		return dockerswarm.ServiceCreateResponse{ID: "svc-created"}, nil
	}
	mock.NetworkRemoveFn = func(_ context.Context, networkID string) error {
		removedNetworks[networkID] = true
		return nil
	}
	mock.NetworkCreateFn = func(_ context.Context, name string, opts dockernetwork.CreateOptions) (dockernetwork.CreateResponse, error) {
		createdNetworks[name] = opts
		return dockernetwork.CreateResponse{ID: "net-created"}, nil
	}
	mock.VolumeRemoveFn = func(_ context.Context, volumeID string, force bool) error {
		if !force {
			t.Fatal("expected force=true for volume removal")
		}
		removedVolumes[volumeID] = true
		return nil
	}
	mock.VolumeCreateFn = func(_ context.Context, opts dockervolume.CreateOptions) (dockervolume.Volume, error) {
		createdVolumes[opts.Name] = opts
		return dockervolume.Volume{Name: opts.Name}, nil
	}

	snap := &snapmod.StackSnapshot{
		StackName:     "mystack",
		IsFirstDeploy: false,
		Services: map[string]dockerswarm.Service{
			"web": {ID: "svc-web", Spec: oldWebSpec},
			"api": {ID: "svc-api", Spec: apiSpec},
		},
		Networks: snapshotNetworks,
		Volumes:  snapshotVolumes,
	}

	err := snap.Rollback(context.Background(), mock)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !removedServices["svc-worker"] {
		t.Fatalf("expected new service svc-worker to be removed, got %v", removedServices)
	}
	if updatedSpec, ok := updatedServices["svc-web"]; !ok {
		t.Fatalf("expected svc-web update call, got %v", updatedServices)
	} else if updatedSpec.TaskTemplate.ContainerSpec == nil || updatedSpec.TaskTemplate.ContainerSpec.Image != "nginx:1.24" {
		t.Fatalf("expected restored nginx:1.24 spec, got %+v", updatedSpec.TaskTemplate.ContainerSpec)
	}
	if !createdServices["mystack_api"] {
		t.Fatalf("expected missing service mystack_api to be recreated, got %v", createdServices)
	}

	if !removedNetworks["net-metrics"] {
		t.Fatalf("expected new network net-metrics to be removed, got %v", removedNetworks)
	}
	createdBackendOpts, ok := createdNetworks["mystack_backend"]
	if !ok {
		t.Fatalf("expected missing network mystack_backend to be created, got %v", createdNetworks)
	}
	if createdBackendOpts.Driver != "overlay" || !createdBackendOpts.Attachable || !createdBackendOpts.Internal {
		t.Fatalf("expected preserved network options, got %+v", createdBackendOpts)
	}

	if !removedVolumes["mystack_cache"] {
		t.Fatalf("expected new volume mystack_cache to be removed, got %v", removedVolumes)
	}
	createdLogsOpts, ok := createdVolumes["mystack_logs"]
	if !ok {
		t.Fatalf("expected missing volume mystack_logs to be created, got %v", createdVolumes)
	}
	if createdLogsOpts.Driver != "local" || createdLogsOpts.DriverOpts["type"] != "tmpfs" {
		t.Fatalf("expected preserved volume options, got %+v", createdLogsOpts)
	}
}

func TestRollback_NegativeCaseMissingStackName(t *testing.T) {
	t.Parallel()

	snap := emptySnapshot("")
	err := snap.Rollback(context.Background(), newStateMock(nil, nil, nil))
	if err == nil {
		t.Fatal("expected error when stack name is empty")
	}
}

func TestRollback_NegativeCaseFirstDeployServiceRemoveError(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("remove failed")
	mock := newStateMock(
		[]dockerswarm.Service{
			{
				ID: "svc-web",
				Spec: dockerswarm.ServiceSpec{
					Annotations: dockerswarm.Annotations{Name: "mystack_web"},
				},
			},
		},
		nil,
		nil,
	)
	mock.ServiceRemoveFn = func(_ context.Context, _ string) error {
		return wantErr
	}
	mock.NetworkRemoveFn = func(_ context.Context, _ string) error { return nil }
	mock.VolumeRemoveFn = func(_ context.Context, _ string, _ bool) error { return nil }

	snap := emptySnapshot("mystack")
	snap.IsFirstDeploy = true

	err := snap.Rollback(context.Background(), mock)
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected wrapped remove error %v, got %v", wantErr, err)
	}
}

func TestRollback_NegativeCaseUpdateServiceUpdateError(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("update failed")
	mock := newStateMock(
		[]dockerswarm.Service{
			{
				ID:   "svc-web",
				Meta: dockerswarm.Meta{Version: dockerswarm.Version{Index: 7}},
				Spec: dockerswarm.ServiceSpec{
					Annotations: dockerswarm.Annotations{Name: "mystack_web"},
				},
			},
		},
		nil,
		nil,
	)
	mock.ServiceRemoveFn = func(_ context.Context, _ string) error { return nil }
	mock.ServiceUpdateFn = func(_ context.Context, _ string, _ dockerswarm.Version, _ dockerswarm.ServiceSpec, _ dockertypes.ServiceUpdateOptions) (dockerswarm.ServiceUpdateResponse, error) {
		return dockerswarm.ServiceUpdateResponse{}, wantErr
	}
	mock.ServiceCreateFn = func(_ context.Context, _ dockerswarm.ServiceSpec, _ dockertypes.ServiceCreateOptions) (dockerswarm.ServiceCreateResponse, error) {
		return dockerswarm.ServiceCreateResponse{}, nil
	}
	mock.NetworkRemoveFn = func(_ context.Context, _ string) error { return nil }
	mock.NetworkCreateFn = func(_ context.Context, _ string, _ dockernetwork.CreateOptions) (dockernetwork.CreateResponse, error) {
		return dockernetwork.CreateResponse{}, nil
	}
	mock.VolumeRemoveFn = func(_ context.Context, _ string, _ bool) error { return nil }
	mock.VolumeCreateFn = func(_ context.Context, _ dockervolume.CreateOptions) (dockervolume.Volume, error) {
		return dockervolume.Volume{}, nil
	}

	snap := emptySnapshot("mystack")
	snap.Services["web"] = dockerswarm.Service{
		Spec: dockerswarm.ServiceSpec{
			Annotations: dockerswarm.Annotations{Name: "mystack_web"},
		},
	}

	err := snap.Rollback(context.Background(), mock)
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected wrapped update error %v, got %v", wantErr, err)
	}
}

func TestRollback_NegativeCaseUpdateNetworkCreateError(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("network create failed")
	mock := newStateMock(nil, nil, nil)
	mock.ServiceRemoveFn = func(_ context.Context, _ string) error { return nil }
	mock.ServiceUpdateFn = func(_ context.Context, _ string, _ dockerswarm.Version, _ dockerswarm.ServiceSpec, _ dockertypes.ServiceUpdateOptions) (dockerswarm.ServiceUpdateResponse, error) {
		return dockerswarm.ServiceUpdateResponse{}, nil
	}
	mock.ServiceCreateFn = func(_ context.Context, _ dockerswarm.ServiceSpec, _ dockertypes.ServiceCreateOptions) (dockerswarm.ServiceCreateResponse, error) {
		return dockerswarm.ServiceCreateResponse{}, nil
	}
	mock.NetworkRemoveFn = func(_ context.Context, _ string) error { return nil }
	mock.NetworkCreateFn = func(_ context.Context, _ string, _ dockernetwork.CreateOptions) (dockernetwork.CreateResponse, error) {
		return dockernetwork.CreateResponse{}, wantErr
	}
	mock.VolumeRemoveFn = func(_ context.Context, _ string, _ bool) error { return nil }
	mock.VolumeCreateFn = func(_ context.Context, _ dockervolume.CreateOptions) (dockervolume.Volume, error) {
		return dockervolume.Volume{}, nil
	}

	snap := emptySnapshot("mystack")
	snap.Networks["backend"] = dockernetwork.Summary{
		Name:   "mystack_backend",
		Driver: "overlay",
	}

	err := snap.Rollback(context.Background(), mock)
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected wrapped network create error %v, got %v", wantErr, err)
	}
}

func TestRollback_NegativeCaseUpdateVolumeCreateError(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("volume create failed")
	mock := newStateMock(nil, nil, nil)
	mock.ServiceRemoveFn = func(_ context.Context, _ string) error { return nil }
	mock.ServiceUpdateFn = func(_ context.Context, _ string, _ dockerswarm.Version, _ dockerswarm.ServiceSpec, _ dockertypes.ServiceUpdateOptions) (dockerswarm.ServiceUpdateResponse, error) {
		return dockerswarm.ServiceUpdateResponse{}, nil
	}
	mock.ServiceCreateFn = func(_ context.Context, _ dockerswarm.ServiceSpec, _ dockertypes.ServiceCreateOptions) (dockerswarm.ServiceCreateResponse, error) {
		return dockerswarm.ServiceCreateResponse{}, nil
	}
	mock.NetworkRemoveFn = func(_ context.Context, _ string) error { return nil }
	mock.NetworkCreateFn = func(_ context.Context, _ string, _ dockernetwork.CreateOptions) (dockernetwork.CreateResponse, error) {
		return dockernetwork.CreateResponse{}, nil
	}
	mock.VolumeRemoveFn = func(_ context.Context, _ string, _ bool) error { return nil }
	mock.VolumeCreateFn = func(_ context.Context, _ dockervolume.CreateOptions) (dockervolume.Volume, error) {
		return dockervolume.Volume{}, wantErr
	}

	snap := emptySnapshot("mystack")
	snap.Volumes["logs"] = dockervolume.Volume{
		Name:   "mystack_logs",
		Driver: "local",
	}

	err := snap.Rollback(context.Background(), mock)
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected wrapped volume create error %v, got %v", wantErr, err)
	}
}
