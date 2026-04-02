package swarm

import (
	"context"
	"fmt"
	"sort"

	dockertypes "github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/swarm"

	"github.com/SomeBlackMagic/stackman/internal/compose"
	"github.com/SomeBlackMagic/stackman/internal/deployment"
)

type ServiceUpdateResult struct {
	ServiceID   string
	ServiceName string
	Version     swarm.Version
	Warnings    []string
	Created     bool
	DeployID    string
}

// DeployServices creates or updates all stack services and injects deploy ID label.
func DeployServices(
	ctx context.Context,
	client DockerClient,
	stackName string,
	services map[string]*compose.Service,
	deployID string,
) ([]ServiceUpdateResult, error) {
	existingServices, err := client.ServiceList(ctx, dockertypes.ServiceListOptions{
		Filters: filters.NewArgs(filters.Arg("label", stackNamespaceLabel+"="+stackName)),
	})
	if err != nil {
		return nil, fmt.Errorf("list existing services: %w", err)
	}

	existingByName := make(map[string]swarm.Service, len(existingServices))
	for _, existing := range existingServices {
		existingByName[existing.Spec.Name] = existing
	}

	isMultiNode, err := IsMultiNodeSwarm(ctx, client)
	if err != nil {
		return nil, fmt.Errorf("detect swarm mode: %w", err)
	}

	gatewayIP := ""
	if !isMultiNode {
		// Best effort for single-node swarm; deployment should continue if detection fails.
		detectedGatewayIP, detectErr := DetectHostGatewayIP(ctx, client)
		if detectErr == nil {
			gatewayIP = detectedGatewayIP
		}
	}

	names := make([]string, 0, len(services))
	for name := range services {
		names = append(names, name)
	}
	sort.Strings(names)

	results := make([]ServiceUpdateResult, 0, len(services))
	for _, name := range names {
		svc := services[name]
		if svc == nil {
			return nil, fmt.Errorf("deploy service %q: nil service config", name)
		}

		serviceConfig := *svc
		if gatewayIP != "" && compose.HasHostGateway(serviceConfig.ExtraHosts) {
			serviceConfig.ExtraHosts = compose.ReplaceHostGatewayToken(serviceConfig.ExtraHosts, gatewayIP)
		}

		spec, err := compose.ConvertToSwarmSpec(name, serviceConfig, stackName)
		if err != nil {
			return nil, fmt.Errorf("deploy service %q: convert spec: %w", name, err)
		}

		if spec.Annotations.Labels == nil {
			spec.Annotations.Labels = make(map[string]string)
		}
		spec.Annotations.Labels[deployment.DeployIDLabel] = deployID

		if spec.TaskTemplate.ContainerSpec != nil {
			if spec.TaskTemplate.ContainerSpec.Labels == nil {
				spec.TaskTemplate.ContainerSpec.Labels = make(map[string]string)
			}
			spec.TaskTemplate.ContainerSpec.Labels[deployment.DeployIDLabel] = deployID
		}

		fullName := stackName + "_" + name
		result := ServiceUpdateResult{
			ServiceName: fullName,
			DeployID:    deployID,
		}

		if existing, ok := existingByName[fullName]; ok {
			resp, err := client.ServiceUpdate(ctx, existing.ID, existing.Meta.Version, spec, dockertypes.ServiceUpdateOptions{})
			if err != nil {
				return nil, fmt.Errorf("deploy service %q: service update: %w", name, err)
			}

			result.ServiceID = existing.ID
			result.Version = existing.Meta.Version
			result.Warnings = append([]string(nil), resp.Warnings...)
			result.Created = false
		} else {
			resp, err := client.ServiceCreate(ctx, spec, dockertypes.ServiceCreateOptions{})
			if err != nil {
				return nil, fmt.Errorf("deploy service %q: service create: %w", name, err)
			}

			result.ServiceID = resp.ID
			result.Created = true
		}

		results = append(results, result)
	}

	return results, nil
}
