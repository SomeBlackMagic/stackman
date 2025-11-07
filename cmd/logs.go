package cmd

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
)

// ExecuteLogs runs the logs command
func ExecuteLogs(args []string) {
	fs := flag.NewFlagSet("logs", flag.ExitOnError)

	// Required flags
	stackName := fs.String("n", "", "Stack name (required)")

	// Optional flags
	serviceName := fs.String("service", "", "Service name (optional, shows logs for all services if not specified)")
	follow := fs.Bool("follow", false, "Follow log output")
	tail := fs.String("tail", "100", "Number of lines to show from the end of the logs")
	since := fs.String("since", "", "Show logs since timestamp (e.g. 2023-01-01T00:00:00Z) or duration (e.g. 10m)")
	timestamps := fs.Bool("timestamps", false, "Show timestamps")

	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, `Usage: stackman logs -n <stack> [flags]

Show logs for stack services and their tasks.

Flags:
`)
		fs.PrintDefaults()
	}

	if err := fs.Parse(args); err != nil {
		os.Exit(1)
	}

	// Validate required flags
	if *stackName == "" {
		fmt.Fprintf(os.Stderr, "Error: -n (stack name) is required\n\n")
		fs.Usage()
		os.Exit(1)
	}

	// Run logs logic
	if err := runLogs(*stackName, &LogsOptions{
		ServiceName: *serviceName,
		Follow:      *follow,
		Tail:        *tail,
		Since:       *since,
		Timestamps:  *timestamps,
	}); err != nil {
		log.Fatalf("Logs failed: %v", err)
	}
}

// LogsOptions contains options for the logs command
type LogsOptions struct {
	ServiceName string
	Follow      bool
	Tail        string
	Since       string
	Timestamps  bool
}

// runLogs retrieves and displays logs for stack services
func runLogs(stackName string, opts *LogsOptions) error {
	ctx := context.Background()

	// Initialize Docker client
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return fmt.Errorf("docker client init: %w", err)
	}
	defer cli.Close()

	// Get services in the stack
	stackLabel := fmt.Sprintf("com.docker.stack.namespace=%s", stackName)
	serviceFilters := filters.NewArgs()
	serviceFilters.Add("label", stackLabel)

	services, err := cli.ServiceList(ctx, types.ServiceListOptions{
		Filters: serviceFilters,
	})
	if err != nil {
		return fmt.Errorf("failed to list services: %w", err)
	}

	if len(services) == 0 {
		fmt.Printf("No services found in stack '%s'\n", stackName)
		return nil
	}

	// Filter by service name if specified
	var targetServices []string
	for _, svc := range services {
		serviceName := svc.Spec.Name
		// Remove stack prefix
		if strings.HasPrefix(serviceName, stackName+"_") {
			serviceName = serviceName[len(stackName)+1:]
		}

		if opts.ServiceName == "" || serviceName == opts.ServiceName {
			targetServices = append(targetServices, svc.ID)
		}
	}

	if len(targetServices) == 0 {
		if opts.ServiceName != "" {
			return fmt.Errorf("service '%s' not found in stack '%s'", opts.ServiceName, stackName)
		}
		return fmt.Errorf("no services found")
	}

	// Get tasks for these services
	taskFilters := filters.NewArgs()
	for _, svcID := range targetServices {
		taskFilters.Add("service", svcID)
	}

	tasks, err := cli.TaskList(ctx, types.TaskListOptions{
		Filters: taskFilters,
	})
	if err != nil {
		return fmt.Errorf("failed to list tasks: %w", err)
	}

	if len(tasks) == 0 {
		fmt.Printf("No tasks found for services in stack '%s'\n", stackName)
		return nil
	}

	// Show logs for each task's container
	for _, task := range tasks {
		// Skip tasks without containers
		if task.Status.ContainerStatus == nil || task.Status.ContainerStatus.ContainerID == "" {
			continue
		}

		// Skip non-running tasks unless we're showing history
		if task.Status.State != "running" && task.Status.State != "complete" {
			continue
		}

		containerID := task.Status.ContainerStatus.ContainerID

		// Get service name for display
		var serviceName string
		for _, svc := range services {
			if svc.ID == task.ServiceID {
				serviceName = svc.Spec.Name
				if strings.HasPrefix(serviceName, stackName+"_") {
					serviceName = serviceName[len(stackName)+1:]
				}
				break
			}
		}

		fmt.Printf("==> Logs for service: %s, task: %s (container: %s)\n",
			serviceName, task.ID[:12], containerID[:12])

		// Prepare log options
		logOpts := container.LogsOptions{
			ShowStdout: true,
			ShowStderr: true,
			Follow:     opts.Follow,
			Timestamps: opts.Timestamps,
			Tail:       opts.Tail,
		}

		// Parse since if provided
		if opts.Since != "" {
			// Try parsing as duration first
			if duration, err := time.ParseDuration(opts.Since); err == nil {
				logOpts.Since = time.Now().Add(-duration).Format(time.RFC3339Nano)
			} else {
				// Otherwise use as-is (timestamp)
				logOpts.Since = opts.Since
			}
		}

		// Get logs
		logReader, err := cli.ContainerLogs(ctx, containerID, logOpts)
		if err != nil {
			log.Printf("Warning: failed to get logs for container %s: %v", containerID[:12], err)
			continue
		}

		// Copy logs to stdout
		// Docker multiplexes stdout/stderr, we need to handle the stream format
		if _, err := io.Copy(os.Stdout, logReader); err != nil {
			log.Printf("Warning: error reading logs for container %s: %v", containerID[:12], err)
		}

		logReader.Close()
		fmt.Println() // Add newline between containers
	}

	return nil
}
