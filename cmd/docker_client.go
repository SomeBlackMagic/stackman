package cmd

import (
	"github.com/docker/docker/client"
)

// newDockerClient creates a Docker client with API version negotiation enabled.
func newDockerClient() (*client.Client, error) {
	return client.NewClientWithOpts(
		client.FromEnv,
		client.WithAPIVersionNegotiation(),
	)
}
