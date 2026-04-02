package compose

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	networktypes "github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/swarm"
	volumetypes "github.com/docker/docker/api/types/volume"
	"github.com/docker/go-units"
)

func ConvertToSwarmSpec(serviceName string, svc Service, stackName string) (swarm.ServiceSpec, error) {
	hostname := svc.Hostname
	if hostname == "" {
		hostname = serviceName
	}

	spec := swarm.ServiceSpec{
		Annotations: swarm.Annotations{
			Name: fmt.Sprintf("%s_%s", stackName, serviceName),
			Labels: map[string]string{
				"com.docker.stack.namespace": stackName,
				"com.docker.stack.image":     svc.Image,
			},
		},
		TaskTemplate: swarm.TaskSpec{
			ContainerSpec: &swarm.ContainerSpec{
				Image:      svc.Image,
				Hostname:   hostname,
				User:       svc.User,
				Dir:        svc.WorkingDir,
				ReadOnly:   svc.ReadOnly,
				StopSignal: svc.StopSignal,
				Isolation:  container.Isolation(svc.Isolation),
				Init:       svc.Init,
				OpenStdin:  svc.StdinOpen,
				TTY:        svc.Tty,
			},
		},
	}

	if svc.Labels != nil {
		for key, value := range svc.Labels {
			spec.Annotations.Labels[key] = value
		}
	}

	replicas := uint64(1)
	spec.Mode = swarm.ServiceMode{
		Replicated: &swarm.ReplicatedService{Replicas: &replicas},
	}

	if svc.StopGracePeriod != "" {
		duration, err := time.ParseDuration(svc.StopGracePeriod)
		if err != nil {
			return swarm.ServiceSpec{}, fmt.Errorf("parse stop_grace_period: %w", err)
		}
		spec.TaskTemplate.ContainerSpec.StopGracePeriod = &duration
	}

	if svc.Domainname != "" {
		spec.TaskTemplate.ContainerSpec.Hostname = hostname + "." + svc.Domainname
	}

	if svc.DNS != nil || svc.DNSSearch != nil || len(svc.DNSOptions) > 0 {
		dnsConfig := &swarm.DNSConfig{}
		if svc.DNS != nil {
			nameservers, err := convertToStringSlice(svc.DNS)
			if err != nil {
				return swarm.ServiceSpec{}, fmt.Errorf("convert dns: %w", err)
			}
			dnsConfig.Nameservers = nameservers
		}
		if svc.DNSSearch != nil {
			search, err := convertToStringSlice(svc.DNSSearch)
			if err != nil {
				return swarm.ServiceSpec{}, fmt.Errorf("convert dns_search: %w", err)
			}
			dnsConfig.Search = search
		}
		if len(svc.DNSOptions) > 0 {
			dnsConfig.Options = append([]string(nil), svc.DNSOptions...)
		}
		spec.TaskTemplate.ContainerSpec.DNSConfig = dnsConfig
	}

	if len(svc.ExtraHosts) > 0 {
		spec.TaskTemplate.ContainerSpec.Hosts = append([]string(nil), svc.ExtraHosts...)
	}

	if len(svc.CapAdd) > 0 {
		spec.TaskTemplate.ContainerSpec.CapabilityAdd = append([]string(nil), svc.CapAdd...)
	}
	if len(svc.CapDrop) > 0 {
		spec.TaskTemplate.ContainerSpec.CapabilityDrop = append([]string(nil), svc.CapDrop...)
	}

	if svc.Environment != nil {
		env, err := convertEnvironment(svc.Environment)
		if err != nil {
			return swarm.ServiceSpec{}, fmt.Errorf("convert environment: %w", err)
		}
		spec.TaskTemplate.ContainerSpec.Env = env
	}

	if svc.Entrypoint != nil {
		entrypoint, err := convertCommand(svc.Entrypoint)
		if err != nil {
			return swarm.ServiceSpec{}, fmt.Errorf("convert entrypoint: %w", err)
		}
		spec.TaskTemplate.ContainerSpec.Command = entrypoint
	}
	if svc.Command != nil {
		command, err := convertCommand(svc.Command)
		if err != nil {
			return swarm.ServiceSpec{}, fmt.Errorf("convert command: %w", err)
		}
		spec.TaskTemplate.ContainerSpec.Args = command
	}

	if svc.Deploy != nil {
		if err := convertDeploy(&spec, svc.Deploy); err != nil {
			return swarm.ServiceSpec{}, fmt.Errorf("convert deploy: %w", err)
		}
	}

	if len(svc.Ports) > 0 {
		ports, err := convertPorts(svc.Ports)
		if err != nil {
			return swarm.ServiceSpec{}, fmt.Errorf("convert ports: %w", err)
		}
		spec.EndpointSpec = &swarm.EndpointSpec{Ports: ports}
	}

	if svc.Deploy != nil && svc.Deploy.Labels != nil {
		for key, value := range svc.Deploy.Labels {
			spec.Annotations.Labels[key] = value
		}
	}

	if svc.HealthCheck != nil {
		healthcheck, err := convertHealthCheck(svc.HealthCheck)
		if err != nil {
			return swarm.ServiceSpec{}, fmt.Errorf("convert healthcheck: %w", err)
		}
		spec.TaskTemplate.ContainerSpec.Healthcheck = healthcheck
	}

	return spec, nil
}

func ConvertComposeToSwarmSpecs(cf *ComposeFile, stackName string) (map[string]swarm.ServiceSpec, error) {
	if cf == nil {
		return nil, fmt.Errorf("compose file is nil")
	}

	specs := make(map[string]swarm.ServiceSpec, len(cf.Services))
	for serviceName, service := range cf.Services {
		if service == nil {
			return nil, fmt.Errorf("service %q is nil", serviceName)
		}

		spec, err := ConvertToSwarmSpec(serviceName, *service, stackName)
		if err != nil {
			return nil, fmt.Errorf("convert service %q: %w", serviceName, err)
		}

		specs[serviceName] = spec
	}

	return specs, nil
}

func convertEnvironment(env interface{}) ([]string, error) {
	switch values := env.(type) {
	case []string:
		return append([]string(nil), values...), nil
	case []interface{}:
		result := make([]string, 0, len(values))
		for _, item := range values {
			value, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("unsupported environment list item: %T", item)
			}
			result = append(result, value)
		}
		return result, nil
	case map[string]string:
		keys := make([]string, 0, len(values))
		for key := range values {
			keys = append(keys, key)
		}
		sort.Strings(keys)

		result := make([]string, 0, len(values))
		for _, key := range keys {
			result = append(result, fmt.Sprintf("%s=%s", key, values[key]))
		}
		return result, nil
	case map[string]interface{}:
		keys := make([]string, 0, len(values))
		for key := range values {
			keys = append(keys, key)
		}
		sort.Strings(keys)

		result := make([]string, 0, len(values))
		for _, key := range keys {
			result = append(result, fmt.Sprintf("%s=%v", key, values[key]))
		}
		return result, nil
	default:
		return nil, fmt.Errorf("unsupported environment type: %T", env)
	}
}

func convertCommand(command interface{}) ([]string, error) {
	switch value := command.(type) {
	case string:
		return strings.Fields(value), nil
	case []string:
		return append([]string(nil), value...), nil
	case []interface{}:
		result := make([]string, 0, len(value))
		for _, item := range value {
			stringValue, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("unsupported command list item: %T", item)
			}
			result = append(result, stringValue)
		}
		return result, nil
	default:
		return nil, fmt.Errorf("unsupported command type: %T", command)
	}
}

func convertHealthCheck(hc *HealthCheck) (*container.HealthConfig, error) {
	if hc.Disable {
		return &container.HealthConfig{Test: []string{"NONE"}}, nil
	}

	config := &container.HealthConfig{}

	switch v := hc.Test.(type) {
	case string:
		config.Test = []string{"CMD-SHELL", v}
	case []string:
		config.Test = append([]string(nil), v...)
	case []interface{}:
		test := make([]string, 0, len(v))
		for _, item := range v {
			if value, ok := item.(string); ok {
				test = append(test, value)
			}
		}
		config.Test = test
	}

	if hc.Interval != "" {
		interval, err := time.ParseDuration(hc.Interval)
		if err != nil {
			return nil, fmt.Errorf("parse interval: %w", err)
		}
		config.Interval = interval
	}

	if hc.Timeout != "" {
		timeout, err := time.ParseDuration(hc.Timeout)
		if err != nil {
			return nil, fmt.Errorf("parse timeout: %w", err)
		}
		config.Timeout = timeout
	}

	if hc.Retries > 0 {
		config.Retries = hc.Retries
	}

	if hc.StartPeriod != "" {
		startPeriod, err := time.ParseDuration(hc.StartPeriod)
		if err != nil {
			return nil, fmt.Errorf("parse start_period: %w", err)
		}
		config.StartPeriod = startPeriod
	}

	return config, nil
}

func convertDeploy(spec *swarm.ServiceSpec, deploy *DeployConfig) error {
	switch deploy.Mode {
	case "", "replicated":
		replicas := uint64(1)
		if deploy.Replicas != nil {
			replicas = uint64(*deploy.Replicas)
		}
		spec.Mode = swarm.ServiceMode{
			Replicated: &swarm.ReplicatedService{Replicas: &replicas},
		}
	case "global":
		spec.Mode = swarm.ServiceMode{
			Global: &swarm.GlobalService{},
		}
	default:
		return fmt.Errorf("unsupported deploy mode: %s", deploy.Mode)
	}

	if deploy.UpdateConfig != nil {
		updateConfig, err := convertUpdateConfig(deploy.UpdateConfig)
		if err != nil {
			return fmt.Errorf("convert update_config: %w", err)
		}
		spec.UpdateConfig = updateConfig
	}

	if deploy.RollbackConfig != nil {
		rollbackConfig, err := convertUpdateConfig(deploy.RollbackConfig)
		if err != nil {
			return fmt.Errorf("convert rollback_config: %w", err)
		}
		spec.RollbackConfig = rollbackConfig
	}

	if deploy.RestartPolicy != nil {
		restartPolicy, err := convertRestartPolicy(deploy.RestartPolicy)
		if err != nil {
			return fmt.Errorf("convert restart_policy: %w", err)
		}
		spec.TaskTemplate.RestartPolicy = restartPolicy
	}

	if deploy.Resources != nil {
		resources, err := convertResources(deploy.Resources)
		if err != nil {
			return fmt.Errorf("convert resources: %w", err)
		}
		spec.TaskTemplate.Resources = resources
	}

	if deploy.Placement != nil {
		placement := &swarm.Placement{
			Constraints: append([]string(nil), deploy.Placement.Constraints...),
		}
		if deploy.Placement.MaxReplicas > 0 {
			placement.MaxReplicas = uint64(deploy.Placement.MaxReplicas)
		}
		if len(deploy.Placement.Preferences) > 0 {
			preferences := make([]swarm.PlacementPreference, 0, len(deploy.Placement.Preferences))
			for _, preference := range deploy.Placement.Preferences {
				if preference.Spread == "" {
					continue
				}
				preferences = append(preferences, swarm.PlacementPreference{
					Spread: &swarm.SpreadOver{SpreadDescriptor: preference.Spread},
				})
			}
			placement.Preferences = preferences
		}
		spec.TaskTemplate.Placement = placement
	}

	return nil
}

func convertUpdateConfig(config *UpdateConfig) (*swarm.UpdateConfig, error) {
	updateConfig := &swarm.UpdateConfig{
		Parallelism:     uint64(config.Parallelism),
		FailureAction:   config.FailureAction,
		MaxFailureRatio: float32(config.MaxFailureRatio),
		Order:           config.Order,
	}

	if config.Delay != "" {
		delay, err := time.ParseDuration(config.Delay)
		if err != nil {
			return nil, fmt.Errorf("parse delay: %w", err)
		}
		updateConfig.Delay = delay
	}

	if config.Monitor != "" {
		monitor, err := time.ParseDuration(config.Monitor)
		if err != nil {
			return nil, fmt.Errorf("parse monitor: %w", err)
		}
		updateConfig.Monitor = monitor
	}

	return updateConfig, nil
}

func convertRestartPolicy(policy *RestartPolicy) (*swarm.RestartPolicy, error) {
	restartPolicy := &swarm.RestartPolicy{
		Condition: swarm.RestartPolicyCondition(policy.Condition),
	}

	if policy.Delay != "" {
		delay, err := time.ParseDuration(policy.Delay)
		if err != nil {
			return nil, fmt.Errorf("parse delay: %w", err)
		}
		restartPolicy.Delay = &delay
	}

	if policy.MaxAttempts != nil {
		attempts := uint64(*policy.MaxAttempts)
		restartPolicy.MaxAttempts = &attempts
	}

	if policy.Window != "" {
		window, err := time.ParseDuration(policy.Window)
		if err != nil {
			return nil, fmt.Errorf("parse window: %w", err)
		}
		restartPolicy.Window = &window
	}

	return restartPolicy, nil
}

func convertResources(resources *Resources) (*swarm.ResourceRequirements, error) {
	requirements := &swarm.ResourceRequirements{}

	if resources.Limits != nil {
		limits, err := convertLimits(resources.Limits)
		if err != nil {
			return nil, fmt.Errorf("convert limits: %w", err)
		}
		requirements.Limits = limits
	}

	if resources.Reservations != nil {
		reservations, err := convertReservations(resources.Reservations)
		if err != nil {
			return nil, fmt.Errorf("convert reservations: %w", err)
		}
		requirements.Reservations = reservations
	}

	return requirements, nil
}

func convertLimits(limits *ResourceLimit) (*swarm.Limit, error) {
	result := &swarm.Limit{}

	if limits.CPUs != "" {
		cpus, err := strconv.ParseFloat(limits.CPUs, 64)
		if err != nil {
			return nil, fmt.Errorf("parse cpus: %w", err)
		}
		result.NanoCPUs = int64(cpus * 1e9)
	}

	if limits.Memory != "" {
		memory, err := units.FromHumanSize(limits.Memory)
		if err != nil {
			return nil, fmt.Errorf("parse memory: %w", err)
		}
		result.MemoryBytes = memory
	}

	return result, nil
}

func convertReservations(reservations *ResourceLimit) (*swarm.Resources, error) {
	result := &swarm.Resources{}

	if reservations.CPUs != "" {
		cpus, err := strconv.ParseFloat(reservations.CPUs, 64)
		if err != nil {
			return nil, fmt.Errorf("parse cpus: %w", err)
		}
		result.NanoCPUs = int64(cpus * 1e9)
	}

	if reservations.Memory != "" {
		memory, err := units.FromHumanSize(reservations.Memory)
		if err != nil {
			return nil, fmt.Errorf("parse memory: %w", err)
		}
		result.MemoryBytes = memory
	}

	return result, nil
}

func convertPorts(ports []interface{}) ([]swarm.PortConfig, error) {
	result := make([]swarm.PortConfig, 0, len(ports))

	for _, port := range ports {
		switch value := port.(type) {
		case string:
			portConfig, err := parsePortString(value)
			if err != nil {
				return nil, err
			}
			result = append(result, *portConfig)
		case map[string]interface{}:
			portConfig, err := parsePortMap(value)
			if err != nil {
				return nil, err
			}
			result = append(result, *portConfig)
		default:
			return nil, fmt.Errorf("unsupported port type: %T", port)
		}
	}

	return result, nil
}

func parsePortMap(port map[string]interface{}) (*swarm.PortConfig, error) {
	targetValue, ok := port["target"]
	if !ok {
		return nil, fmt.Errorf("port target is required")
	}
	targetPort, err := interfaceToUint32(targetValue)
	if err != nil {
		return nil, fmt.Errorf("parse target port: %w", err)
	}

	publishedPort := targetPort
	if publishedValue, ok := port["published"]; ok {
		publishedPort, err = interfaceToUint32(publishedValue)
		if err != nil {
			return nil, fmt.Errorf("parse published port: %w", err)
		}
	}

	protocol := swarm.PortConfigProtocolTCP
	if protocolValue, ok := port["protocol"]; ok {
		protocolString, ok := protocolValue.(string)
		if !ok {
			return nil, fmt.Errorf("invalid protocol type: %T", protocolValue)
		}
		protocol = swarm.PortConfigProtocol(protocolString)
	}

	publishMode := swarm.PortConfigPublishModeIngress
	if modeValue, ok := port["mode"]; ok {
		modeString, ok := modeValue.(string)
		if !ok {
			return nil, fmt.Errorf("invalid mode type: %T", modeValue)
		}
		publishMode = swarm.PortConfigPublishMode(modeString)
	}

	return &swarm.PortConfig{
		TargetPort:    targetPort,
		PublishedPort: publishedPort,
		Protocol:      protocol,
		PublishMode:   publishMode,
	}, nil
}

func parsePortString(portString string) (*swarm.PortConfig, error) {
	config := &swarm.PortConfig{
		Protocol:    swarm.PortConfigProtocolTCP,
		PublishMode: swarm.PortConfigPublishModeIngress,
	}

	parts := strings.Split(portString, "/")
	portPart := parts[0]
	if len(parts) > 1 {
		config.Protocol = swarm.PortConfigProtocol(parts[1])
	}

	portParts := strings.Split(portPart, ":")
	switch len(portParts) {
	case 1:
		targetPort, err := strconv.ParseUint(portParts[0], 10, 32)
		if err != nil {
			return nil, fmt.Errorf("invalid target port: %w", err)
		}
		config.TargetPort = uint32(targetPort)
		config.PublishedPort = uint32(targetPort)
	case 2:
		publishedPort, err := strconv.ParseUint(portParts[0], 10, 32)
		if err != nil {
			return nil, fmt.Errorf("invalid published port: %w", err)
		}
		targetPort, err := strconv.ParseUint(portParts[1], 10, 32)
		if err != nil {
			return nil, fmt.Errorf("invalid target port: %w", err)
		}
		config.PublishedPort = uint32(publishedPort)
		config.TargetPort = uint32(targetPort)
	default:
		return nil, fmt.Errorf("invalid port format: %s", portString)
	}

	return config, nil
}

func interfaceToUint32(value interface{}) (uint32, error) {
	switch v := value.(type) {
	case int:
		return uint32(v), nil
	case int64:
		return uint32(v), nil
	case uint64:
		return uint32(v), nil
	case float64:
		return uint32(v), nil
	case string:
		parsed, err := strconv.ParseUint(v, 10, 32)
		if err != nil {
			return 0, fmt.Errorf("parse uint32: %w", err)
		}
		return uint32(parsed), nil
	default:
		return 0, fmt.Errorf("unsupported number type: %T", value)
	}
}

func convertToStringSlice(input interface{}) ([]string, error) {
	switch values := input.(type) {
	case string:
		return []string{values}, nil
	case []string:
		return append([]string(nil), values...), nil
	case []interface{}:
		result := make([]string, 0, len(values))
		for _, item := range values {
			stringValue, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("unsupported string slice item: %T", item)
			}
			result = append(result, stringValue)
		}
		return result, nil
	default:
		return nil, fmt.Errorf("unsupported type for string slice: %T", input)
	}
}

func ConvertNetwork(name string, netCfg Network, stackName string) (networktypes.CreateOptions, error) {
	driver := netCfg.Driver
	if driver == "" {
		driver = "overlay"
	}

	labels := map[string]string{
		"com.docker.stack.namespace": stackName,
	}
	for key, value := range netCfg.Labels {
		labels[key] = value
	}

	return networktypes.CreateOptions{
		Driver:     driver,
		Attachable: netCfg.Attachable,
		Labels:     labels,
		Options:    netCfg.DriverOpts,
	}, nil
}

func ConvertVolume(name string, vol Volume, stackName string) (volumetypes.CreateOptions, error) {
	driver := vol.Driver
	if driver == "" {
		driver = "local"
	}

	labels := map[string]string{
		"com.docker.stack.namespace": stackName,
	}
	for key, value := range vol.Labels {
		labels[key] = value
	}

	return volumetypes.CreateOptions{
		Driver:     driver,
		DriverOpts: vol.DriverOpts,
		Labels:     labels,
		Name:       name,
	}, nil
}
