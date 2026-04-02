package compose

// ComposeFile represents the structure of a docker-compose.yml file
type ComposeFile struct {
	Version  string              `yaml:"version"`
	Services map[string]*Service `yaml:"services"`
	Networks map[string]*Network `yaml:"networks,omitempty"`
	Volumes  map[string]*Volume  `yaml:"volumes,omitempty"`
	Secrets  map[string]*Secret  `yaml:"secrets,omitempty"`
	Configs  map[string]*Config  `yaml:"configs,omitempty"`
}

type Service struct {
	Image           string                 `yaml:"image,omitempty"`
	Build           *BuildConfig           `yaml:"build,omitempty"`
	Command         interface{}            `yaml:"command,omitempty"`
	Entrypoint      interface{}            `yaml:"entrypoint,omitempty"`
	Environment     interface{}            `yaml:"environment,omitempty"`
	EnvFile         interface{}            `yaml:"env_file,omitempty"`
	Ports           []interface{}          `yaml:"ports,omitempty"`
	Expose          []interface{}          `yaml:"expose,omitempty"`
	Volumes         []interface{}          `yaml:"volumes,omitempty"`
	VolumesFrom     []string               `yaml:"volumes_from,omitempty"`
	Networks        interface{}            `yaml:"networks,omitempty"`
	Deploy          *DeployConfig          `yaml:"deploy,omitempty"`
	DependsOn       interface{}            `yaml:"depends_on,omitempty"`
	Labels          map[string]string      `yaml:"labels,omitempty"`
	HealthCheck     *HealthCheck           `yaml:"healthcheck,omitempty"`
	Secrets         []interface{}          `yaml:"secrets,omitempty"`
	Configs         []interface{}          `yaml:"configs,omitempty"`
	Hostname        string                 `yaml:"hostname,omitempty"`
	Domainname      string                 `yaml:"domainname,omitempty"`
	User            string                 `yaml:"user,omitempty"`
	WorkingDir      string                 `yaml:"working_dir,omitempty"`
	Privileged      bool                   `yaml:"privileged,omitempty"`
	Restart         string                 `yaml:"restart,omitempty"`
	StdinOpen       bool                   `yaml:"stdin_open,omitempty"`
	Tty             bool                   `yaml:"tty,omitempty"`
	ReadOnly        bool                   `yaml:"read_only,omitempty"`
	StopSignal      string                 `yaml:"stop_signal,omitempty"`
	StopGracePeriod string                 `yaml:"stop_grace_period,omitempty"`
	Tmpfs           interface{}            `yaml:"tmpfs,omitempty"`
	Ulimits         map[string]interface{} `yaml:"ulimits,omitempty"`
	DNS             interface{}            `yaml:"dns,omitempty"`
	DNSSearch       interface{}            `yaml:"dns_search,omitempty"`
	DNSOptions      []string               `yaml:"dns_opt,omitempty"`
	ExtraHosts      []string               `yaml:"extra_hosts,omitempty"`
	MacAddress      string                 `yaml:"mac_address,omitempty"`
	Logging         *LoggingConfig         `yaml:"logging,omitempty"`
	CapAdd          []string               `yaml:"cap_add,omitempty"`
	CapDrop         []string               `yaml:"cap_drop,omitempty"`
	SecurityOpt     []string               `yaml:"security_opt,omitempty"`
	StorageOpt      map[string]string      `yaml:"storage_opt,omitempty"`
	Sysctls         interface{}            `yaml:"sysctls,omitempty"`
	Isolation       string                 `yaml:"isolation,omitempty"`
	Init            *bool                  `yaml:"init,omitempty"`
	PidMode         string                 `yaml:"pid,omitempty"`
	IpcMode         string                 `yaml:"ipc,omitempty"`
	CgroupParent    string                 `yaml:"cgroup_parent,omitempty"`
	Devices         []string               `yaml:"devices,omitempty"`
	Links           []string               `yaml:"links,omitempty"`
	ExternalLinks   []string               `yaml:"external_links,omitempty"`
}

type BuildConfig struct {
	Context    string            `yaml:"context,omitempty"`
	Dockerfile string            `yaml:"dockerfile,omitempty"`
	Args       map[string]string `yaml:"args,omitempty"`
	Target     string            `yaml:"target,omitempty"`
	CacheFrom  []string          `yaml:"cache_from,omitempty"`
	Labels     map[string]string `yaml:"labels,omitempty"`
	Network    string            `yaml:"network,omitempty"`
	ShmSize    string            `yaml:"shm_size,omitempty"`
	ExtraHosts []string          `yaml:"extra_hosts,omitempty"`
}

type LoggingConfig struct {
	Driver  string            `yaml:"driver,omitempty"`
	Options map[string]string `yaml:"options,omitempty"`
}

type DeployConfig struct {
	Mode           string            `yaml:"mode,omitempty"`
	Replicas       *int              `yaml:"replicas,omitempty"`
	Labels         map[string]string `yaml:"labels,omitempty"`
	UpdateConfig   *UpdateConfig     `yaml:"update_config,omitempty"`
	RollbackConfig *UpdateConfig     `yaml:"rollback_config,omitempty"`
	Resources      *Resources        `yaml:"resources,omitempty"`
	RestartPolicy  *RestartPolicy    `yaml:"restart_policy,omitempty"`
	Placement      *Placement        `yaml:"placement,omitempty"`
	EndpointMode   string            `yaml:"endpoint_mode,omitempty"`
}

type UpdateConfig struct {
	Parallelism     int     `yaml:"parallelism,omitempty"`
	Delay           string  `yaml:"delay,omitempty"`
	FailureAction   string  `yaml:"failure_action,omitempty"`
	Monitor         string  `yaml:"monitor,omitempty"`
	MaxFailureRatio float64 `yaml:"max_failure_ratio,omitempty"`
	Order           string  `yaml:"order,omitempty"`
}

type Resources struct {
	Limits       *ResourceLimit `yaml:"limits,omitempty"`
	Reservations *ResourceLimit `yaml:"reservations,omitempty"`
}

type ResourceLimit struct {
	CPUs             string            `yaml:"cpus,omitempty"`
	Memory           string            `yaml:"memory,omitempty"`
	Pids             int64             `yaml:"pids,omitempty"`
	Devices          []DeviceRequest   `yaml:"devices,omitempty"`
	GenericResources []GenericResource `yaml:"generic_resources,omitempty"`
}

type DeviceRequest struct {
	Capabilities []string          `yaml:"capabilities,omitempty"`
	Driver       string            `yaml:"driver,omitempty"`
	Count        int               `yaml:"count,omitempty"`
	DeviceIDs    []string          `yaml:"device_ids,omitempty"`
	Options      map[string]string `yaml:"options,omitempty"`
}

type GenericResource struct {
	DiscreteResourceSpec *DiscreteResourceSpec `yaml:"discrete_resource_spec,omitempty"`
}

type DiscreteResourceSpec struct {
	Kind  string `yaml:"kind,omitempty"`
	Value int64  `yaml:"value,omitempty"`
}

type RestartPolicy struct {
	Condition   string `yaml:"condition,omitempty"`
	Delay       string `yaml:"delay,omitempty"`
	MaxAttempts *int   `yaml:"max_attempts,omitempty"`
	Window      string `yaml:"window,omitempty"`
}

type Placement struct {
	Constraints []string              `yaml:"constraints,omitempty"`
	Preferences []PlacementPreference `yaml:"preferences,omitempty"`
	MaxReplicas int                   `yaml:"max_replicas_per_node,omitempty"`
	Platforms   []Platform            `yaml:"platforms,omitempty"`
}

type PlacementPreference struct {
	Spread string `yaml:"spread,omitempty"`
}

type Platform struct {
	Architecture string `yaml:"architecture,omitempty"`
	OS           string `yaml:"os,omitempty"`
}

type HealthCheck struct {
	Test        interface{} `yaml:"test,omitempty"`
	Interval    string      `yaml:"interval,omitempty"`
	Timeout     string      `yaml:"timeout,omitempty"`
	Retries     int         `yaml:"retries,omitempty"`
	StartPeriod string      `yaml:"start_period,omitempty"`
	Disable     bool        `yaml:"disable,omitempty"`
}

type Network struct {
	Driver     string            `yaml:"driver,omitempty"`
	DriverOpts map[string]string `yaml:"driver_opts,omitempty"`
	External   interface{}       `yaml:"external,omitempty"`
	Labels     map[string]string `yaml:"labels,omitempty"`
	Attachable bool              `yaml:"attachable,omitempty"`
	Internal   bool              `yaml:"internal,omitempty"`
	EnableIPv6 bool              `yaml:"enable_ipv6,omitempty"`
	IPAM       *IPAMConfig       `yaml:"ipam,omitempty"`
}

type IPAMConfig struct {
	Driver  string            `yaml:"driver,omitempty"`
	Config  []IPAMPool        `yaml:"config,omitempty"`
	Options map[string]string `yaml:"options,omitempty"`
}

type IPAMPool struct {
	Subnet     string            `yaml:"subnet,omitempty"`
	IPRange    string            `yaml:"ip_range,omitempty"`
	Gateway    string            `yaml:"gateway,omitempty"`
	AuxAddress map[string]string `yaml:"aux_addresses,omitempty"`
}

type Volume struct {
	Driver     string            `yaml:"driver,omitempty"`
	DriverOpts map[string]string `yaml:"driver_opts,omitempty"`
	External   interface{}       `yaml:"external,omitempty"`
	Labels     map[string]string `yaml:"labels,omitempty"`
}

type Secret struct {
	File     string            `yaml:"file,omitempty"`
	External interface{}       `yaml:"external,omitempty"`
	Labels   map[string]string `yaml:"labels,omitempty"`
}

type Config struct {
	File     string            `yaml:"file,omitempty"`
	External interface{}       `yaml:"external,omitempty"`
	Labels   map[string]string `yaml:"labels,omitempty"`
}
