//go:build integration
// +build integration

package main

import (
	"context"
	"os/exec"
	"strings"
	"testing"

	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/client"
)

// TestIntegration_ExtraHosts_HostGateway проверяет что "host-gateway" в extra_hosts
// корректно разрешается в реальный IP на одноузловом Swarm.
func TestIntegration_ExtraHosts_HostGateway(t *testing.T) {
	if !isDockerAvailable(t) {
		t.Skip("Docker is not available or not in swarm mode")
	}

	stackName := "stackman-extra-hosts-test"
	cleanup := func() { cleanupStack(t, stackName) }
	cleanup()
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	t.Log("Deploying stack with extra_hosts host-gateway...")
	cmd := exec.CommandContext(ctx, "../stackman", "apply",
		"-n", stackName,
		"-f", "testdata/extra-hosts-stack.yml",
		"-timeout", "2m",
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Deployment with extra_hosts host-gateway failed: %v\nOutput: %s", err, output)
	}
	t.Logf("Deploy output: %s", output)

	// Создаём Docker-клиент для инспекции
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		t.Fatalf("Failed to create Docker client: %v", err)
	}
	defer cli.Close()

	// Находим сервис в стеке
	services, err := cli.ServiceList(ctx, swarm.ServiceListOptions{
		Filters: filters.NewArgs(filters.Arg("label", "com.docker.stack.namespace="+stackName)),
	})
	if err != nil {
		t.Fatalf("Failed to list services: %v", err)
	}
	if len(services) == 0 {
		t.Fatal("No services found for stack")
	}

	// Проверяем что ContainerSpec.Hosts содержит реальный IP, а не "host-gateway"
	svc := services[0]
	hosts := svc.Spec.TaskTemplate.ContainerSpec.Hosts
	t.Logf("ContainerSpec.Hosts: %v", hosts)

	if len(hosts) == 0 {
		t.Fatal("ContainerSpec.Hosts is empty, expected at least one entry")
	}

	for _, h := range hosts {
		if strings.Contains(h, "host-gateway") {
			t.Errorf("host-gateway was not resolved: found literal %q in ContainerSpec.Hosts", h)
		}
		if strings.Contains(h, "host.docker.internal") {
			t.Logf("Found expected host entry: %q", h)
		}
	}

	// Дополнительно: убеждаемся что IP не пустой (запись должна быть валидным форматом)
	found := false
	for _, h := range hosts {
		if strings.Contains(h, "host.docker.internal") {
			found = true
			// IP должен присутствовать и не быть пустым
			if h == "host.docker.internal:" || h == ":host.docker.internal" {
				t.Errorf("host entry has empty IP: %q", h)
			}
		}
	}
	if !found {
		t.Errorf("no entry for 'host.docker.internal' found in ContainerSpec.Hosts: %v", hosts)
	}
}
