package deployer

import (
	"context"
	"fmt"
	"log"

	"github.com/docker/docker/api/types/volume"

	"stackman/compose"
)

func (d *StackDeployer) createVolumes(ctx context.Context, volumes map[string]*compose.Volume) error {
	for name, volConfig := range volumes {
		fullName := fmt.Sprintf("%s_%s", d.stackName, name)

		// Check if volume already exists
		_, err := d.cli.VolumeInspect(ctx, fullName)
		if err == nil {
			log.Printf("Volume %s already exists", fullName)
			continue
		}

		// Create volume
		driver := "local"
		if volConfig != nil && volConfig.Driver != "" {
			driver = volConfig.Driver
		}

		labels := map[string]string{
			"com.docker.stack.namespace": d.stackName,
		}
		if volConfig != nil && volConfig.Labels != nil {
			for k, v := range volConfig.Labels {
				labels[k] = v
			}
		}

		opts := volume.CreateOptions{
			Driver: driver,
			Labels: labels,
		}

		if volConfig != nil && volConfig.DriverOpts != nil {
			opts.DriverOpts = volConfig.DriverOpts
		}

		_, err = d.cli.VolumeCreate(ctx, opts)
		if err != nil {
			return fmt.Errorf("failed to create volume %s: %w", fullName, err)
		}

		log.Printf("Created volume: %s", fullName)
	}

	return nil
}
