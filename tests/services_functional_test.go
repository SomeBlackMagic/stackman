//go:build integration

package tests_test

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	dockertypes "github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/network"
	swarmtypes "github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/api/types/volume"
	"github.com/docker/docker/client"
	"github.com/docker/go-units"

	"github.com/SomeBlackMagic/stackman/internal/compose"
	"github.com/SomeBlackMagic/stackman/internal/deployment"
	swarmint "github.com/SomeBlackMagic/stackman/internal/swarm"
)

func TestDeployServices_FunctionalCreateAndUpdate(t *testing.T) {
	cli := newDockerClient(t)
	t.Cleanup(func() { _ = cli.Close() })

	requireSwarm(t, cli)

	stackName := uniqueStackName(t, "images-services")
	cleanupStackResources(t, cli, stackName)
	cleanupStackServices(t, cli, stackName)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	adapter := &dockerClientAdapter{Client: cli}

	networks := map[string]*compose.Network{
		"backend": {
			Driver:     "overlay",
			Attachable: true,
		},
	}
	volumes := map[string]*compose.Volume{
		"data": {
			Driver: "local",
		},
	}

	if err := swarmint.EnsureNetworks(ctx, adapter, stackName, networks); err != nil {
		t.Fatalf("ensure networks failed: %v", err)
	}
	assertStackNetworkExists(t, ctx, cli, stackName, "backend")

	if err := swarmint.EnsureVolumes(ctx, adapter, stackName, volumes); err != nil {
		t.Fatalf("ensure volumes failed: %v", err)
	}
	assertStackVolumeExists(t, ctx, cli, stackName, "data")

	publishedPort := uniquePublishedPort()
	createServices := map[string]*compose.Service{
		"web": buildFunctionalServiceConfig(publishedPort, "1200", "v1", "15s"),
	}

	firstDeployID := "deploy-functional-create"
	if err := swarmint.PullImages(ctx, adapter, createServices); err != nil {
		t.Fatalf("pull images before create failed: %v", err)
	}

	createResults, err := swarmint.DeployServices(ctx, adapter, stackName, createServices, firstDeployID)
	if err != nil {
		t.Fatalf("create deploy failed: %v", err)
	}
	if len(createResults) != 1 {
		t.Fatalf("expected one result for create, got %d", len(createResults))
	}
	if !createResults[0].Created {
		t.Fatalf("expected Created=true on first deploy, got result: %+v", createResults[0])
	}
	if err := waitForServiceRunning(ctx, cli, createResults[0].ServiceID, 1); err != nil {
		t.Fatalf("created service did not reach running state: %v", err)
	}

	createdService, _, err := cli.ServiceInspectWithRaw(ctx, createResults[0].ServiceID, dockertypes.ServiceInspectOptions{})
	if err != nil {
		t.Fatalf("inspect created service: %v", err)
	}
	createdVersion := createdService.Meta.Version.Index
	assertServiceSettings(t, createdService, expectedServiceSettings{
		DeployID:       firstDeployID,
		AppVersion:     "v1",
		SleepSeconds:   "1200",
		HealthInterval: 15 * time.Second,
		PublishedPort:  publishedPort,
	})

	updateServices := map[string]*compose.Service{
		"web": buildFunctionalServiceConfig(publishedPort, "1400", "v2", "20s"),
	}

	secondDeployID := "deploy-functional-update"
	if err := swarmint.PullImages(ctx, adapter, updateServices); err != nil {
		t.Fatalf("pull images before update failed: %v", err)
	}

	updateResults, err := swarmint.DeployServices(ctx, adapter, stackName, updateServices, secondDeployID)
	if err != nil {
		t.Fatalf("update deploy failed: %v", err)
	}
	if len(updateResults) != 1 {
		t.Fatalf("expected one result for update, got %d", len(updateResults))
	}
	if updateResults[0].Created {
		t.Fatalf("expected Created=false on second deploy, got result: %+v", updateResults[0])
	}
	if updateResults[0].ServiceID != createResults[0].ServiceID {
		t.Fatalf("expected service ID to stay same, got create=%q update=%q", createResults[0].ServiceID, updateResults[0].ServiceID)
	}
	if err := waitForServiceRunning(ctx, cli, updateResults[0].ServiceID, 1); err != nil {
		t.Fatalf("updated service did not reach running state: %v", err)
	}

	updatedService, _, err := cli.ServiceInspectWithRaw(ctx, updateResults[0].ServiceID, dockertypes.ServiceInspectOptions{})
	if err != nil {
		t.Fatalf("inspect updated service: %v", err)
	}
	assertServiceSettings(t, updatedService, expectedServiceSettings{
		DeployID:       secondDeployID,
		AppVersion:     "v2",
		SleepSeconds:   "1400",
		HealthInterval: 20 * time.Second,
		PublishedPort:  publishedPort,
	})

	if updatedService.Meta.Version.Index <= createdVersion {
		t.Fatalf("expected service version to increase after update, created=%d updated=%d", createdVersion, updatedService.Meta.Version.Index)
	}
}

func buildFunctionalServiceConfig(publishedPort uint32, sleepSeconds, appVersion, healthInterval string) *compose.Service {
	replicas := 1
	maxAttempts := 3

	return &compose.Service{
		Image:   "alpine:3.20",
		Command: []interface{}{"sleep", sleepSeconds},
		Environment: map[string]interface{}{
			"A_VAR":       "alpha",
			"APP_VERSION": appVersion,
		},
		ExtraHosts: []string{
			"example.local:127.0.0.1",
			"host.docker.internal:host-gateway",
		},
		DNS:       []interface{}{"1.1.1.1", "8.8.8.8"},
		DNSSearch: []interface{}{"localdomain"},
		DNSOptions: []string{
			"ndots:2",
		},
		CapDrop: []string{"NET_RAW"},
		Ports: []interface{}{
			map[string]interface{}{
				"target":    8080,
				"published": int(publishedPort),
				"protocol":  "tcp",
				"mode":      "ingress",
			},
		},
		Labels: map[string]string{
			"app": "stackman-e2e",
		},
		HealthCheck: &compose.HealthCheck{
			Test:        []string{"CMD-SHELL", "true"},
			Interval:    healthInterval,
			Timeout:     "3s",
			Retries:     2,
			StartPeriod: "5s",
		},
		Deploy: &compose.DeployConfig{
			Replicas: &replicas,
			Labels: map[string]string{
				"com.example.role": "worker",
			},
			UpdateConfig: &compose.UpdateConfig{
				Parallelism:     1,
				Delay:           "3s",
				FailureAction:   "pause",
				Monitor:         "10s",
				MaxFailureRatio: 0.2,
				Order:           "start-first",
			},
			RestartPolicy: &compose.RestartPolicy{
				Condition:   "on-failure",
				Delay:       "2s",
				MaxAttempts: &maxAttempts,
				Window:      "30s",
			},
			Resources: &compose.Resources{
				Limits: &compose.ResourceLimit{
					CPUs:   "0.5",
					Memory: "64M",
				},
				Reservations: &compose.ResourceLimit{
					CPUs:   "0.25",
					Memory: "32M",
				},
			},
			Placement: &compose.Placement{
				MaxReplicas: 1,
				Preferences: []compose.PlacementPreference{
					{Spread: "node.id"},
				},
			},
		},
	}
}

func newDockerClient(t *testing.T) *client.Client {
	t.Helper()

	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		t.Fatalf("create docker client: %v", err)
	}
	return cli
}

type dockerClientAdapter struct {
	*client.Client
}

func (a *dockerClientAdapter) VolumeList(ctx context.Context, filter filters.Args) (volume.ListResponse, error) {
	return a.Client.VolumeList(ctx, volume.ListOptions{Filters: filter})
}

func requireSwarm(t *testing.T, cli *client.Client) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	info, err := cli.Info(ctx)
	if err != nil {
		t.Skipf("docker is unavailable: %v", err)
	}
	if info.Swarm.LocalNodeState != "active" {
		t.Skipf("docker swarm is not active (state=%q)", info.Swarm.LocalNodeState)
	}
}

func cleanupStackServices(t *testing.T, cli *client.Client, stackName string) {
	t.Helper()

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		services, err := cli.ServiceList(ctx, dockertypes.ServiceListOptions{
			Filters: filters.NewArgs(filters.Arg("label", "com.docker.stack.namespace="+stackName)),
		})
		if err != nil {
			t.Logf("cleanup skipped: list services failed: %v", err)
			return
		}

		for _, svc := range services {
			if removeErr := cli.ServiceRemove(ctx, svc.ID); removeErr != nil {
				t.Logf("cleanup warning: remove service %q (%s): %v", svc.Spec.Name, svc.ID, removeErr)
			}
		}
	})
}

func cleanupStackResources(t *testing.T, cli *client.Client, stackName string) {
	t.Helper()

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		volumesResp, err := cli.VolumeList(ctx, volume.ListOptions{
			Filters: filters.NewArgs(filters.Arg("label", "com.docker.stack.namespace="+stackName)),
		})
		if err != nil {
			t.Logf("cleanup warning: list volumes failed: %v", err)
		} else {
			for _, vol := range volumesResp.Volumes {
				if removeErr := cli.VolumeRemove(ctx, vol.Name, true); removeErr != nil {
					t.Logf("cleanup warning: remove volume %q: %v", vol.Name, removeErr)
				}
			}
		}

		networks, err := cli.NetworkList(ctx, network.ListOptions{
			Filters: filters.NewArgs(filters.Arg("label", "com.docker.stack.namespace="+stackName)),
		})
		if err != nil {
			t.Logf("cleanup warning: list networks failed: %v", err)
			return
		}
		for _, net := range networks {
			if removeErr := cli.NetworkRemove(ctx, net.ID); removeErr != nil {
				t.Logf("cleanup warning: remove network %q (%s): %v", net.Name, net.ID, removeErr)
			}
		}
	})
}

func uniqueStackName(t *testing.T, prefix string) string {
	t.Helper()
	return fmt.Sprintf("stackman-%s-%d", prefix, time.Now().UnixNano())
}

func uniquePublishedPort() uint32 {
	// High range to minimize conflicts on shared CI hosts.
	return uint32(45000 + time.Now().UnixNano()%1500)
}

func assertStackNetworkExists(t *testing.T, ctx context.Context, cli *client.Client, stackName, networkName string) {
	t.Helper()

	networks, err := cli.NetworkList(ctx, network.ListOptions{
		Filters: filters.NewArgs(filters.Arg("label", "com.docker.stack.namespace="+stackName)),
	})
	if err != nil {
		t.Fatalf("list stack networks: %v", err)
	}

	wantName := stackName + "_" + networkName
	for _, net := range networks {
		if net.Name == wantName {
			return
		}
	}
	t.Fatalf("expected network %q to exist in stack, got %d networks", wantName, len(networks))
}

func assertStackVolumeExists(t *testing.T, ctx context.Context, cli *client.Client, stackName, volumeName string) {
	t.Helper()

	volumesResp, err := cli.VolumeList(ctx, volume.ListOptions{
		Filters: filters.NewArgs(filters.Arg("label", "com.docker.stack.namespace="+stackName)),
	})
	if err != nil {
		t.Fatalf("list stack volumes: %v", err)
	}

	wantName := stackName + "_" + volumeName
	for _, vol := range volumesResp.Volumes {
		if vol.Name == wantName {
			return
		}
	}
	t.Fatalf("expected volume %q to exist in stack, got %d volumes", wantName, len(volumesResp.Volumes))
}

type expectedServiceSettings struct {
	DeployID       string
	AppVersion     string
	SleepSeconds   string
	HealthInterval time.Duration
	PublishedPort  uint32
}

func assertServiceSettings(t *testing.T, svc swarmtypes.Service, expected expectedServiceSettings) {
	t.Helper()

	assertDeployLabel(t, svc.Spec.Labels[deployment.DeployIDLabel], expected.DeployID)
	if got := svc.Spec.Labels["app"]; got != "stackman-e2e" {
		t.Fatalf("expected service label app=stackman-e2e, got %q", got)
	}
	if got := svc.Spec.Labels["com.example.role"]; got != "worker" {
		t.Fatalf("expected deploy label com.example.role=worker, got %q", got)
	}
	if got := svc.Spec.Labels["com.docker.stack.image"]; got != "alpine:3.20" {
		t.Fatalf("expected stack image label alpine:3.20, got %q", got)
	}

	containerSpec := svc.Spec.TaskTemplate.ContainerSpec
	if containerSpec == nil {
		t.Fatal("expected container spec")
	}

	assertDeployLabel(t, containerSpec.Labels[deployment.DeployIDLabel], expected.DeployID)
	if containerSpec.Image != "alpine:3.20" {
		t.Fatalf("expected image alpine:3.20, got %q", containerSpec.Image)
	}

	if len(containerSpec.Args) != 2 || containerSpec.Args[0] != "sleep" || containerSpec.Args[1] != expected.SleepSeconds {
		t.Fatalf("unexpected command args: %v", containerSpec.Args)
	}

	env := envSliceToMap(containerSpec.Env)
	if env["A_VAR"] != "alpha" {
		t.Fatalf("expected A_VAR=alpha, got %q", env["A_VAR"])
	}
	if env["APP_VERSION"] != expected.AppVersion {
		t.Fatalf("expected APP_VERSION=%s, got %q", expected.AppVersion, env["APP_VERSION"])
	}

	if containerSpec.DNSConfig == nil {
		t.Fatal("expected dns config to be set")
	}
	assertContains(t, containerSpec.DNSConfig.Nameservers, "1.1.1.1", "dns nameservers")
	assertContains(t, containerSpec.DNSConfig.Nameservers, "8.8.8.8", "dns nameservers")
	assertContains(t, containerSpec.DNSConfig.Search, "localdomain", "dns search")
	assertContains(t, containerSpec.DNSConfig.Options, "ndots:2", "dns options")

	assertContains(t, containerSpec.Hosts, "example.local:127.0.0.1", "extra hosts")
	assertHasPrefixedHost(t, containerSpec.Hosts, "host.docker.internal:")
	assertContains(t, containerSpec.CapabilityDrop, "NET_RAW", "capability drop")

	if containerSpec.Healthcheck == nil {
		t.Fatal("expected healthcheck to be set")
	}
	if len(containerSpec.Healthcheck.Test) != 2 || containerSpec.Healthcheck.Test[0] != "CMD-SHELL" || containerSpec.Healthcheck.Test[1] != "true" {
		t.Fatalf("unexpected healthcheck test: %v", containerSpec.Healthcheck.Test)
	}
	if containerSpec.Healthcheck.Interval != expected.HealthInterval {
		t.Fatalf("expected healthcheck interval %s, got %s", expected.HealthInterval, containerSpec.Healthcheck.Interval)
	}
	if containerSpec.Healthcheck.Timeout != 3*time.Second {
		t.Fatalf("expected healthcheck timeout 3s, got %s", containerSpec.Healthcheck.Timeout)
	}
	if containerSpec.Healthcheck.Retries != 2 {
		t.Fatalf("expected healthcheck retries 2, got %d", containerSpec.Healthcheck.Retries)
	}
	if containerSpec.Healthcheck.StartPeriod != 5*time.Second {
		t.Fatalf("expected healthcheck start period 5s, got %s", containerSpec.Healthcheck.StartPeriod)
	}

	if svc.Spec.EndpointSpec == nil || len(svc.Spec.EndpointSpec.Ports) != 1 {
		t.Fatalf("expected one published port in endpoint spec, got %+v", svc.Spec.EndpointSpec)
	}
	port := svc.Spec.EndpointSpec.Ports[0]
	if port.TargetPort != 8080 {
		t.Fatalf("expected target port 8080, got %d", port.TargetPort)
	}
	if port.PublishedPort != expected.PublishedPort {
		t.Fatalf("expected published port %d, got %d", expected.PublishedPort, port.PublishedPort)
	}
	if port.Protocol != swarmtypes.PortConfigProtocolTCP {
		t.Fatalf("expected tcp protocol, got %s", port.Protocol)
	}
	if port.PublishMode != swarmtypes.PortConfigPublishModeIngress {
		t.Fatalf("expected ingress publish mode, got %s", port.PublishMode)
	}

	if svc.Spec.UpdateConfig == nil {
		t.Fatal("expected update config")
	}
	if svc.Spec.UpdateConfig.Parallelism != 1 {
		t.Fatalf("expected update parallelism 1, got %d", svc.Spec.UpdateConfig.Parallelism)
	}
	if svc.Spec.UpdateConfig.Delay != 3*time.Second {
		t.Fatalf("expected update delay 3s, got %s", svc.Spec.UpdateConfig.Delay)
	}
	if svc.Spec.UpdateConfig.FailureAction != "pause" {
		t.Fatalf("expected update failure action pause, got %q", svc.Spec.UpdateConfig.FailureAction)
	}
	if svc.Spec.UpdateConfig.Monitor != 10*time.Second {
		t.Fatalf("expected update monitor 10s, got %s", svc.Spec.UpdateConfig.Monitor)
	}
	if svc.Spec.UpdateConfig.Order != "start-first" {
		t.Fatalf("expected update order start-first, got %q", svc.Spec.UpdateConfig.Order)
	}
	if svc.Spec.UpdateConfig.MaxFailureRatio != float32(0.2) {
		t.Fatalf("expected max failure ratio 0.2, got %v", svc.Spec.UpdateConfig.MaxFailureRatio)
	}

	if svc.Spec.TaskTemplate.RestartPolicy == nil {
		t.Fatal("expected restart policy")
	}
	if svc.Spec.TaskTemplate.RestartPolicy.Condition != swarmtypes.RestartPolicyCondition("on-failure") {
		t.Fatalf("expected restart condition on-failure, got %q", svc.Spec.TaskTemplate.RestartPolicy.Condition)
	}
	if svc.Spec.TaskTemplate.RestartPolicy.Delay == nil || *svc.Spec.TaskTemplate.RestartPolicy.Delay != 2*time.Second {
		t.Fatalf("expected restart delay 2s, got %+v", svc.Spec.TaskTemplate.RestartPolicy.Delay)
	}
	if svc.Spec.TaskTemplate.RestartPolicy.MaxAttempts == nil || *svc.Spec.TaskTemplate.RestartPolicy.MaxAttempts != 3 {
		t.Fatalf("expected restart max attempts 3, got %+v", svc.Spec.TaskTemplate.RestartPolicy.MaxAttempts)
	}
	if svc.Spec.TaskTemplate.RestartPolicy.Window == nil || *svc.Spec.TaskTemplate.RestartPolicy.Window != 30*time.Second {
		t.Fatalf("expected restart window 30s, got %+v", svc.Spec.TaskTemplate.RestartPolicy.Window)
	}

	if svc.Spec.TaskTemplate.Placement == nil {
		t.Fatal("expected placement settings")
	}
	if svc.Spec.TaskTemplate.Placement.MaxReplicas != 1 {
		t.Fatalf("expected max replicas per node = 1, got %d", svc.Spec.TaskTemplate.Placement.MaxReplicas)
	}
	if len(svc.Spec.TaskTemplate.Placement.Preferences) != 1 {
		t.Fatalf("expected one placement preference, got %d", len(svc.Spec.TaskTemplate.Placement.Preferences))
	}
	if svc.Spec.TaskTemplate.Placement.Preferences[0].Spread == nil || svc.Spec.TaskTemplate.Placement.Preferences[0].Spread.SpreadDescriptor != "node.id" {
		t.Fatalf("expected placement spread node.id, got %+v", svc.Spec.TaskTemplate.Placement.Preferences[0].Spread)
	}

	assertServiceResources(t, svc, "0.5", "64M", "0.25", "32M")
}

func envSliceToMap(env []string) map[string]string {
	result := make(map[string]string, len(env))
	for _, item := range env {
		parts := strings.SplitN(item, "=", 2)
		if len(parts) != 2 {
			continue
		}
		result[parts[0]] = parts[1]
	}
	return result
}

func assertDeployLabel(t *testing.T, got, want string) {
	t.Helper()
	if got != want {
		t.Fatalf("expected %s=%q, got %q", deployment.DeployIDLabel, want, got)
	}
}

func assertContains(t *testing.T, values []string, want, field string) {
	t.Helper()
	for _, value := range values {
		if value == want {
			return
		}
	}
	t.Fatalf("expected %q in %s, got %v", want, field, values)
}

func assertHasPrefixedHost(t *testing.T, hosts []string, prefix string) {
	t.Helper()
	for _, host := range hosts {
		if strings.HasPrefix(host, prefix) {
			suffix := strings.TrimPrefix(host, prefix)
			if suffix == "" {
				t.Fatalf("expected non-empty host value after prefix %q, got %q", prefix, host)
			}
			return
		}
	}
	t.Fatalf("expected host entry with prefix %q, got %v", prefix, hosts)
}

func assertServiceResources(t *testing.T, svc swarmtypes.Service, limitCPU, limitMem, reservationCPU, reservationMem string) {
	t.Helper()

	resources := svc.Spec.TaskTemplate.Resources
	if resources == nil {
		t.Fatal("expected TaskTemplate.Resources to be set")
	}
	if resources.Limits == nil {
		t.Fatal("expected resources limits to be set")
	}
	if resources.Reservations == nil {
		t.Fatal("expected resources reservations to be set")
	}

	wantLimitCPU := parseCPUNano(t, limitCPU)
	wantReservationCPU := parseCPUNano(t, reservationCPU)
	wantLimitMem, err := units.FromHumanSize(limitMem)
	if err != nil {
		t.Fatalf("parse expected limit memory %q: %v", limitMem, err)
	}
	wantReservationMem, err := units.FromHumanSize(reservationMem)
	if err != nil {
		t.Fatalf("parse expected reservation memory %q: %v", reservationMem, err)
	}

	if resources.Limits.NanoCPUs != wantLimitCPU {
		t.Fatalf("expected limits NanoCPUs=%d, got %d", wantLimitCPU, resources.Limits.NanoCPUs)
	}
	if resources.Reservations.NanoCPUs != wantReservationCPU {
		t.Fatalf("expected reservations NanoCPUs=%d, got %d", wantReservationCPU, resources.Reservations.NanoCPUs)
	}
	if resources.Limits.MemoryBytes != wantLimitMem {
		t.Fatalf("expected limits MemoryBytes=%d, got %d", wantLimitMem, resources.Limits.MemoryBytes)
	}
	if resources.Reservations.MemoryBytes != wantReservationMem {
		t.Fatalf("expected reservations MemoryBytes=%d, got %d", wantReservationMem, resources.Reservations.MemoryBytes)
	}
}

func parseCPUNano(t *testing.T, cpu string) int64 {
	t.Helper()

	value, err := strconv.ParseFloat(cpu, 64)
	if err != nil {
		t.Fatalf("parse expected cpu %q: %v", cpu, err)
	}
	return int64(value * 1e9)
}

func waitForServiceRunning(ctx context.Context, cli *client.Client, serviceID string, expectedRunning int) error {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		tasks, err := cli.TaskList(ctx, dockertypes.TaskListOptions{
			Filters: filters.NewArgs(filters.Arg("service", serviceID)),
		})
		if err != nil {
			return fmt.Errorf("list tasks for service %s: %w", serviceID, err)
		}

		running := 0
		for _, task := range tasks {
			if task.DesiredState == swarmtypes.TaskStateRunning && task.Status.State == swarmtypes.TaskStateRunning {
				running++
			}
		}
		if running >= expectedRunning {
			return nil
		}

		select {
		case <-ctx.Done():
			return fmt.Errorf("wait for %d running tasks for service %s: %w", expectedRunning, serviceID, ctx.Err())
		case <-ticker.C:
		}
	}
}
