package cmd

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/docker/docker/api/types"
	flag "github.com/spf13/pflag"

	"github.com/SomeBlackMagic/stackman/internal/swarm"
)

// ExecuteRollback runs the rollback command
func ExecuteRollback(args []string) {
	fs := flag.NewFlagSet("rollback", flag.ExitOnError)

	// Required flags
	stackName := fs.StringP("name", "n", "", "Stack name (required)")

	// Optional flags
	rollbackTimeout := fs.Duration("rollback-timeout", 10*time.Minute, "Rollback timeout")
	// TODO: Add --snapshot flag for manual rollback from saved snapshot

	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, `Usage: stackman rollback -n <stack> [flags]

Rollback stack services to their previous state.

Note: This command performs automatic rollback using Docker Swarm's built-in
rollback mechanism. For custom rollback from snapshot, use apply with appropriate
compose file.

Flags:
`)
		fs.PrintDefaults()
	}

	if err := fs.Parse(args); err != nil {
		os.Exit(1)
	}

	// Validate required flags
	if *stackName == "" {
		fmt.Fprintf(os.Stderr, "Error: -n/--name (stack name) is required\n\n")
		fs.Usage()
		os.Exit(1)
	}

	// Run rollback logic
	if err := runRollback(*stackName, &RollbackOptions{
		Timeout: *rollbackTimeout,
	}); err != nil {
		log.Fatalf("Rollback failed: %v", err)
		os.Exit(3) // Exit code 3 for rollback failure
	}
}

// RollbackOptions contains options for the rollback command
type RollbackOptions struct {
	Timeout time.Duration
}

// runRollback performs automatic rollback of stack services
func runRollback(stackName string, opts *RollbackOptions) error {
	ctx, cancel := context.WithTimeout(context.Background(), opts.Timeout)
	defer cancel()

	// Initialize Docker client
	cli, err := newDockerClient()
	if err != nil {
		return fmt.Errorf("docker client init: %w", err)
	}
	defer cli.Close()

	log.Printf("Starting rollback for stack: %s", stackName)

	// Create deployer
	stackDeployer := swarm.NewStackDeployerWithLogger(cli, stackName, 3, log.Default())

	// Get current services
	services, err := stackDeployer.GetStackServices(ctx)
	if err != nil {
		return fmt.Errorf("failed to list services: %w", err)
	}

	if len(services) == 0 {
		log.Printf("No services found in stack '%s'", stackName)
		return nil
	}

	log.Printf("Found %d services to rollback", len(services))

	// Perform rollback using Docker Swarm's built-in rollback
	rollbackCount := 0
	for _, svc := range services {
		// Check if service has previous spec to rollback to
		if svc.PreviousSpec == nil {
			log.Printf("Service %s has no previous spec, skipping", svc.Spec.Name)
			continue
		}

		log.Printf("Rolling back service: %s", svc.Spec.Name)

		// Trigger Docker Swarm's automatic rollback
		_, err := cli.ServiceUpdate(
			ctx,
			svc.ID,
			svc.Version,
			*svc.PreviousSpec,
			types.ServiceUpdateOptions{
				RegistryAuthFrom: types.RegistryAuthFromPreviousSpec,
			},
		)
		if err != nil {
			log.Printf("Warning: failed to rollback service %s: %v", svc.Spec.Name, err)
			continue
		}

		log.Printf("✅ Service %s rollback initiated", svc.Spec.Name)
		rollbackCount++
	}

	if rollbackCount == 0 {
		log.Printf("No services were rolled back")
		return nil
	}

	log.Printf("Rollback completed: %d services rolled back", rollbackCount)
	fmt.Printf("\nRollback initiated for %d service(s). Use 'stackman status -n %s' to monitor progress.\n", rollbackCount, stackName)

	return nil
}
