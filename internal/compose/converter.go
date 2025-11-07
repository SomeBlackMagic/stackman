package compose

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/go-units"

	"stackman/internal/paths"
)

// ConvertToSwarmSpec converts a compose service to Docker Swarm ServiceSpec
func ConvertToSwarmSpec(serviceName string, service *Service, stackName string) (*swarm.ServiceSpec, error) {
	// Set hostname: use service name if not specified
	hostname := service.Hostname
	if hostname == "" {
		hostname = serviceName
	}

	spec := &swarm.ServiceSpec{
		Annotations: swarm.Annotations{
			Name: fmt.Sprintf("%s_%s", stackName, serviceName),
			Labels: map[string]string{
				"com.docker.stack.namespace": stackName,
			},
		},
		TaskTemplate: swarm.TaskSpec{
			ContainerSpec: &swarm.ContainerSpec{
				Image:      service.Image,
				Hostname:   hostname,
				User:       service.User,
				Dir:        service.WorkingDir,
				ReadOnly:   service.ReadOnly,
				StopSignal: service.StopSignal,
				Isolation:  container.Isolation(service.Isolation),
				Init:       service.Init,
			},
		},
	}

	// Add custom labels
	if service.Labels != nil {
		for k, v := range service.Labels {
			spec.Annotations.Labels[k] = v
		}
	}

	// Convert StopGracePeriod
	if service.StopGracePeriod != "" {
		duration, err := time.ParseDuration(service.StopGracePeriod)
		if err != nil {
			return nil, fmt.Errorf("invalid stop_grace_period: %w", err)
		}
		spec.TaskTemplate.ContainerSpec.StopGracePeriod = &duration
	}

	// Convert Domainname, Stdin, TTY
	if service.Domainname != "" {
		spec.TaskTemplate.ContainerSpec.Hostname = service.Hostname + "." + service.Domainname
	}
	spec.TaskTemplate.ContainerSpec.OpenStdin = service.StdinOpen
	spec.TaskTemplate.ContainerSpec.TTY = service.Tty

	// Convert DNS
	if service.DNS != nil {
		dns, err := convertToStringSlice(service.DNS)
		if err != nil {
			return nil, fmt.Errorf("failed to convert dns: %w", err)
		}
		spec.TaskTemplate.ContainerSpec.DNSConfig = &swarm.DNSConfig{
			Nameservers: dns,
		}
	}

	// Convert DNSSearch
	if service.DNSSearch != nil {
		dnsSearch, err := convertToStringSlice(service.DNSSearch)
		if err != nil {
			return nil, fmt.Errorf("failed to convert dns_search: %w", err)
		}
		if spec.TaskTemplate.ContainerSpec.DNSConfig == nil {
			spec.TaskTemplate.ContainerSpec.DNSConfig = &swarm.DNSConfig{}
		}
		spec.TaskTemplate.ContainerSpec.DNSConfig.Search = dnsSearch
	}

	// Convert DNSOptions
	if len(service.DNSOptions) > 0 {
		if spec.TaskTemplate.ContainerSpec.DNSConfig == nil {
			spec.TaskTemplate.ContainerSpec.DNSConfig = &swarm.DNSConfig{}
		}
		spec.TaskTemplate.ContainerSpec.DNSConfig.Options = service.DNSOptions
	}

	// Convert ExtraHosts
	if len(service.ExtraHosts) > 0 {
		spec.TaskTemplate.ContainerSpec.Hosts = service.ExtraHosts
	}

	// Convert CapAdd
	if len(service.CapAdd) > 0 {
		spec.TaskTemplate.ContainerSpec.CapabilityAdd = service.CapAdd
	}

	// Convert CapDrop
	if len(service.CapDrop) > 0 {
		spec.TaskTemplate.ContainerSpec.CapabilityDrop = service.CapDrop
	}

	// Note: SecurityOpt, Sysctls, and Ulimits are not supported in Docker Swarm API
	// These fields are stored in compose types but won't be applied

	// Convert environment variables
	if service.Environment != nil {
		env, err := convertEnvironment(service.Environment)
		if err != nil {
			return nil, fmt.Errorf("failed to convert environment: %w", err)
		}
		spec.TaskTemplate.ContainerSpec.Env = env
	}

	// Convert command
	if service.Command != nil {
		cmd, err := convertCommand(service.Command)
		if err != nil {
			return nil, fmt.Errorf("failed to convert command: %w", err)
		}
		spec.TaskTemplate.ContainerSpec.Command = cmd
	}

	// Convert entrypoint
	if service.Entrypoint != nil {
		entrypoint, err := convertCommand(service.Entrypoint)
		if err != nil {
			return nil, fmt.Errorf("failed to convert entrypoint: %w", err)
		}
		spec.TaskTemplate.ContainerSpec.Command = entrypoint
	}

	// Convert volumes/mounts
	if len(service.Volumes) > 0 {
		mounts, err := convertVolumes(service.Volumes)
		if err != nil {
			return nil, fmt.Errorf("failed to convert volumes: %w", err)
		}
		spec.TaskTemplate.ContainerSpec.Mounts = mounts
	}

	// Convert healthcheck
	if service.HealthCheck != nil && !service.HealthCheck.Disable {
		healthcheck, err := convertHealthCheck(service.HealthCheck)
		if err != nil {
			return nil, fmt.Errorf("failed to convert healthcheck: %w", err)
		}
		spec.TaskTemplate.ContainerSpec.Healthcheck = healthcheck
	}

	// Convert deploy configuration
	if service.Deploy != nil {
		if err := convertDeploy(spec, service.Deploy); err != nil {
			return nil, fmt.Errorf("failed to convert deploy config: %w", err)
		}
	}

	// Convert ports
	if len(service.Ports) > 0 {
		ports, err := convertPorts(service.Ports)
		if err != nil {
			return nil, fmt.Errorf("failed to convert ports: %w", err)
		}
		spec.EndpointSpec = &swarm.EndpointSpec{
			Ports: ports,
		}
	}

	// Add deploy labels
	if service.Deploy != nil && service.Deploy.Labels != nil {
		for k, v := range service.Deploy.Labels {
			spec.Annotations.Labels[k] = v
		}
	}

	return spec, nil
}

func convertEnvironment(env interface{}) ([]string, error) {
	switch v := env.(type) {
	case []interface{}:
		result := make([]string, 0, len(v))
		for _, item := range v {
			if str, ok := item.(string); ok {
				result = append(result, str)
			}
		}
		return result, nil
	case map[string]interface{}:
		result := make([]string, 0, len(v))
		for key, val := range v {
			result = append(result, fmt.Sprintf("%s=%v", key, val))
		}
		return result, nil
	default:
		return nil, fmt.Errorf("unsupported environment type: %T", env)
	}
}

func convertCommand(cmd interface{}) ([]string, error) {
	switch v := cmd.(type) {
	case string:
		// Shell form: split by spaces (simple approach)
		return strings.Fields(v), nil
	case []interface{}:
		result := make([]string, 0, len(v))
		for _, item := range v {
			if str, ok := item.(string); ok {
				result = append(result, str)
			}
		}
		return result, nil
	default:
		return nil, fmt.Errorf("unsupported command type: %T", cmd)
	}
}

func convertVolumes(volumes []interface{}) ([]mount.Mount, error) {
	var mounts []mount.Mount

	// Create path resolver using SWARM_STACK_PATH or current directory
	resolver, err := paths.NewResolver()
	if err != nil {
		return nil, fmt.Errorf("failed to create path resolver: %w", err)
	}

	for _, vol := range volumes {
		volStr, ok := vol.(string)
		if !ok {
			continue
		}

		parts := strings.Split(volStr, ":")
		if len(parts) < 2 {
			continue
		}

		// Resolve source path using the path resolver
		source := resolver.Resolve(parts[0])

		m := mount.Mount{
			Source: source,
			Target: parts[1],
			Type:   mount.TypeBind,
		}

		// Check for read-only flag
		if len(parts) >= 3 {
			opts := strings.Split(parts[2], ",")
			for _, opt := range opts {
				if opt == "ro" {
					m.ReadOnly = true
				}
			}
		}

		mounts = append(mounts, m)
	}

	return mounts, nil
}

func convertHealthCheck(hc *HealthCheck) (*container.HealthConfig, error) {
	config := &container.HealthConfig{}

	// Convert test
	switch v := hc.Test.(type) {
	case string:
		config.Test = []string{"CMD-SHELL", v}
	case []interface{}:
		test := make([]string, 0, len(v))
		for _, item := range v {
			if str, ok := item.(string); ok {
				test = append(test, str)
			}
		}
		config.Test = test
	}

	// Convert interval
	if hc.Interval != "" {
		interval, err := time.ParseDuration(hc.Interval)
		if err != nil {
			return nil, fmt.Errorf("invalid interval: %w", err)
		}
		config.Interval = interval
	}

	// Convert timeout
	if hc.Timeout != "" {
		timeout, err := time.ParseDuration(hc.Timeout)
		if err != nil {
			return nil, fmt.Errorf("invalid timeout: %w", err)
		}
		config.Timeout = timeout
	}

	// Convert start period
	if hc.StartPeriod != "" {
		startPeriod, err := time.ParseDuration(hc.StartPeriod)
		if err != nil {
			return nil, fmt.Errorf("invalid start_period: %w", err)
		}
		config.StartPeriod = startPeriod
	}

	// Set retries
	if hc.Retries > 0 {
		config.Retries = hc.Retries
	}

	return config, nil
}

func convertDeploy(spec *swarm.ServiceSpec, deploy *DeployConfig) error {
	// Set mode
	if deploy.Mode == "" || deploy.Mode == "replicated" {
		spec.Mode = swarm.ServiceMode{
			Replicated: &swarm.ReplicatedService{},
		}
		if deploy.Replicas != nil {
			replicas := uint64(*deploy.Replicas)
			spec.Mode.Replicated.Replicas = &replicas
		}
	} else if deploy.Mode == "global" {
		spec.Mode = swarm.ServiceMode{
			Global: &swarm.GlobalService{},
		}
	}

	// Convert update config
	if deploy.UpdateConfig != nil {
		spec.UpdateConfig = &swarm.UpdateConfig{
			Parallelism: uint64(deploy.UpdateConfig.Parallelism),
			Order:       deploy.UpdateConfig.Order,
		}

		if deploy.UpdateConfig.Delay != "" {
			delay, err := time.ParseDuration(deploy.UpdateConfig.Delay)
			if err != nil {
				return fmt.Errorf("invalid update delay: %w", err)
			}
			spec.UpdateConfig.Delay = delay
		}

		if deploy.UpdateConfig.FailureAction != "" {
			spec.UpdateConfig.FailureAction = deploy.UpdateConfig.FailureAction
		}

		if deploy.UpdateConfig.Monitor != "" {
			monitor, err := time.ParseDuration(deploy.UpdateConfig.Monitor)
			if err != nil {
				return fmt.Errorf("invalid monitor duration: %w", err)
			}
			spec.UpdateConfig.Monitor = monitor
		}

		if deploy.UpdateConfig.MaxFailureRatio > 0 {
			spec.UpdateConfig.MaxFailureRatio = float32(deploy.UpdateConfig.MaxFailureRatio)
		}
	}

	// Convert restart policy
	if deploy.RestartPolicy != nil {
		spec.TaskTemplate.RestartPolicy = &swarm.RestartPolicy{}

		if deploy.RestartPolicy.Condition != "" {
			spec.TaskTemplate.RestartPolicy.Condition = swarm.RestartPolicyCondition(deploy.RestartPolicy.Condition)
		}

		if deploy.RestartPolicy.Delay != "" {
			delay, err := time.ParseDuration(deploy.RestartPolicy.Delay)
			if err != nil {
				return fmt.Errorf("invalid restart delay: %w", err)
			}
			spec.TaskTemplate.RestartPolicy.Delay = &delay
		}

		if deploy.RestartPolicy.MaxAttempts != nil {
			attempts := uint64(*deploy.RestartPolicy.MaxAttempts)
			spec.TaskTemplate.RestartPolicy.MaxAttempts = &attempts
		}

		if deploy.RestartPolicy.Window != "" {
			window, err := time.ParseDuration(deploy.RestartPolicy.Window)
			if err != nil {
				return fmt.Errorf("invalid restart window: %w", err)
			}
			spec.TaskTemplate.RestartPolicy.Window = &window
		}
	}

	// Convert resources
	if deploy.Resources != nil {
		spec.TaskTemplate.Resources = &swarm.ResourceRequirements{}

		if deploy.Resources.Limits != nil {
			limits := &swarm.Limit{}
			if deploy.Resources.Limits.CPUs != "" {
				cpus, err := strconv.ParseFloat(deploy.Resources.Limits.CPUs, 64)
				if err != nil {
					return fmt.Errorf("invalid CPU limit: %w", err)
				}
				nanoCPUs := int64(cpus * 1e9)
				limits.NanoCPUs = nanoCPUs
			}
			if deploy.Resources.Limits.Memory != "" {
				memory, err := units.FromHumanSize(deploy.Resources.Limits.Memory)
				if err != nil {
					return fmt.Errorf("invalid memory limit: %w", err)
				}
				limits.MemoryBytes = memory
			}
			spec.TaskTemplate.Resources.Limits = limits
		}

		if deploy.Resources.Reservations != nil {
			reservations := &swarm.Resources{}
			if deploy.Resources.Reservations.CPUs != "" {
				cpus, err := strconv.ParseFloat(deploy.Resources.Reservations.CPUs, 64)
				if err != nil {
					return fmt.Errorf("invalid CPU reservation: %w", err)
				}
				nanoCPUs := int64(cpus * 1e9)
				reservations.NanoCPUs = nanoCPUs
			}
			if deploy.Resources.Reservations.Memory != "" {
				memory, err := units.FromHumanSize(deploy.Resources.Reservations.Memory)
				if err != nil {
					return fmt.Errorf("invalid memory reservation: %w", err)
				}
				reservations.MemoryBytes = memory
			}
			spec.TaskTemplate.Resources.Reservations = reservations
		}
	}

	// Convert placement
	if deploy.Placement != nil {
		spec.TaskTemplate.Placement = &swarm.Placement{
			Constraints: deploy.Placement.Constraints,
		}
		if deploy.Placement.MaxReplicas > 0 {
			maxReplicas := uint64(deploy.Placement.MaxReplicas)
			spec.TaskTemplate.Placement.MaxReplicas = maxReplicas
		}
	}

	return nil
}

func convertPorts(ports []interface{}) ([]swarm.PortConfig, error) {
	var result []swarm.PortConfig

	for _, p := range ports {
		switch v := p.(type) {
		case string:
			// Short syntax: "8080:80" or "8080:80/tcp"
			portConfig, err := parsePortString(v)
			if err != nil {
				return nil, err
			}
			result = append(result, *portConfig)

		case map[string]interface{}:
			// Long syntax
			portConfig := &swarm.PortConfig{}

			if target, ok := v["target"].(int); ok {
				portConfig.TargetPort = uint32(target)
			}
			if published, ok := v["published"].(int); ok {
				portConfig.PublishedPort = uint32(published)
			}
			if protocol, ok := v["protocol"].(string); ok {
				portConfig.Protocol = swarm.PortConfigProtocol(protocol)
			} else {
				portConfig.Protocol = swarm.PortConfigProtocolTCP
			}
			if mode, ok := v["mode"].(string); ok {
				portConfig.PublishMode = swarm.PortConfigPublishMode(mode)
			} else {
				portConfig.PublishMode = swarm.PortConfigPublishModeIngress
			}

			result = append(result, *portConfig)
		}
	}

	return result, nil
}

func parsePortString(portStr string) (*swarm.PortConfig, error) {
	config := &swarm.PortConfig{
		Protocol:    swarm.PortConfigProtocolTCP,
		PublishMode: swarm.PortConfigPublishModeIngress,
	}

	// Split by protocol if present
	parts := strings.Split(portStr, "/")
	portPart := parts[0]
	if len(parts) > 1 {
		config.Protocol = swarm.PortConfigProtocol(parts[1])
	}

	// Split published and target ports
	portParts := strings.Split(portPart, ":")
	if len(portParts) == 1 {
		// Only target port specified
		target, err := strconv.ParseUint(portParts[0], 10, 32)
		if err != nil {
			return nil, fmt.Errorf("invalid target port: %w", err)
		}
		config.TargetPort = uint32(target)
		config.PublishedPort = uint32(target)
	} else if len(portParts) == 2 {
		// Published:target format
		published, err := strconv.ParseUint(portParts[0], 10, 32)
		if err != nil {
			return nil, fmt.Errorf("invalid published port: %w", err)
		}
		target, err := strconv.ParseUint(portParts[1], 10, 32)
		if err != nil {
			return nil, fmt.Errorf("invalid target port: %w", err)
		}
		config.PublishedPort = uint32(published)
		config.TargetPort = uint32(target)
	}

	return config, nil
}

func convertToStringSlice(input interface{}) ([]string, error) {
	switch v := input.(type) {
	case []interface{}:
		result := make([]string, 0, len(v))
		for _, item := range v {
			if str, ok := item.(string); ok {
				result = append(result, str)
			}
		}
		return result, nil
	case []string:
		return v, nil
	case string:
		return []string{v}, nil
	default:
		return nil, fmt.Errorf("unsupported type for string slice: %T", input)
	}
}
