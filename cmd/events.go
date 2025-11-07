package cmd

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/events"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
)

// ExecuteEvents runs the events command
func ExecuteEvents(args []string) {
	fs := flag.NewFlagSet("events", flag.ExitOnError)

	// Required flags
	stackName := fs.String("n", "", "Stack name (required)")

	// Optional flags
	serviceName := fs.String("service", "", "Service name (optional, shows events for all services if not specified)")
	since := fs.String("since", "", "Show events since timestamp (e.g. 2023-01-01T00:00:00Z) or duration (e.g. 10m)")
	until := fs.String("until", "", "Show events until timestamp (e.g. 2023-01-01T00:00:00Z) or duration (e.g. 10m)")
	follow := fs.Bool("follow", false, "Follow event stream (default: show past events)")

	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, `Usage: stackman events -n <stack> [flags]

Show events for stack services and their tasks.

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

	// Run events logic
	if err := runEvents(*stackName, &EventsOptions{
		ServiceName: *serviceName,
		Since:       *since,
		Until:       *until,
		Follow:      *follow,
	}); err != nil {
		log.Fatalf("Events failed: %v", err)
	}
}

// EventsOptions contains options for the events command
type EventsOptions struct {
	ServiceName string
	Since       string
	Until       string
	Follow      bool
}

// runEvents retrieves and displays events for stack services and tasks
func runEvents(stackName string, opts *EventsOptions) error {
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

	// Build service name map for display
	serviceNameMap := make(map[string]string)
	for _, svc := range services {
		serviceName := svc.Spec.Name
		shortName := serviceName
		if strings.HasPrefix(serviceName, stackName+"_") {
			shortName = serviceName[len(stackName)+1:]
		}

		// Filter by service name if specified
		if opts.ServiceName != "" && shortName != opts.ServiceName {
			continue
		}

		serviceNameMap[svc.ID] = shortName
	}

	if len(serviceNameMap) == 0 {
		if opts.ServiceName != "" {
			return fmt.Errorf("service '%s' not found in stack '%s'", opts.ServiceName, stackName)
		}
		return fmt.Errorf("no services found")
	}

	// Prepare event filters
	eventFilters := filters.NewArgs()
	eventFilters.Add("type", "service")
	eventFilters.Add("type", "task")
	eventFilters.Add("type", "container")

	// Filter by stack label
	eventFilters.Add("label", stackLabel)

	// Build options for event stream
	eventOpts := events.ListOptions{
		Filters: eventFilters,
	}

	// Parse since/until times
	if opts.Since != "" {
		if duration, err := time.ParseDuration(opts.Since); err == nil {
			eventOpts.Since = time.Now().Add(-duration).Format(time.RFC3339Nano)
		} else {
			eventOpts.Since = opts.Since
		}
	}

	if opts.Until != "" {
		if duration, err := time.ParseDuration(opts.Until); err == nil {
			eventOpts.Until = time.Now().Add(-duration).Format(time.RFC3339Nano)
		} else {
			eventOpts.Until = opts.Until
		}
	}

	// Get event stream
	eventChan, errChan := cli.Events(ctx, eventOpts)

	fmt.Printf("Watching events for stack '%s'...\n", stackName)
	if opts.ServiceName != "" {
		fmt.Printf("  (filtered to service: %s)\n", opts.ServiceName)
	}
	fmt.Println()

	// Process events
	for {
		select {
		case event := <-eventChan:
			displayEvent(event, serviceNameMap, stackName)

		case err := <-errChan:
			if err != nil {
				return fmt.Errorf("error receiving events: %w", err)
			}
			// Channel closed, exit if not following
			if !opts.Follow {
				return nil
			}

		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// displayEvent formats and displays a Docker event
func displayEvent(event events.Message, serviceNameMap map[string]string, stackName string) {
	timestamp := time.Unix(event.Time, 0).Format("2006-01-02 15:04:05")

	switch event.Type {
	case "service":
		serviceName := serviceNameMap[event.Actor.ID]
		if serviceName == "" {
			serviceName = event.Actor.Attributes["name"]
		}

		fmt.Printf("[%s] SERVICE %s: %s",
			timestamp,
			serviceName,
			event.Action,
		)

		// Show additional details for certain actions
		if event.Action == "update" {
			if updateState := event.Actor.Attributes["updatestate.new"]; updateState != "" {
				fmt.Printf(" (state: %s)", updateState)
			}
		}
		fmt.Println()

	case "task":
		taskID := event.Actor.ID[:12]
		serviceName := serviceNameMap[event.Actor.Attributes["com.docker.swarm.service.id"]]
		if serviceName == "" {
			serviceName = event.Actor.Attributes["com.docker.swarm.service.name"]
			if strings.HasPrefix(serviceName, stackName+"_") {
				serviceName = serviceName[len(stackName)+1:]
			}
		}

		fmt.Printf("[%s] TASK %s (%s): %s",
			timestamp,
			taskID,
			serviceName,
			event.Action,
		)

		// Show task state and message
		if state := event.Actor.Attributes["desiredstate"]; state != "" {
			fmt.Printf(" desired=%s", state)
		}
		if state := event.Actor.Attributes["currentstate"]; state != "" {
			fmt.Printf(" current=%s", state)
		}
		if msg := event.Actor.Attributes["message"]; msg != "" {
			fmt.Printf(" msg=\"%s\"", msg)
		}
		if exitCode := event.Actor.Attributes["exitcode"]; exitCode != "" && exitCode != "0" {
			fmt.Printf(" exitcode=%s", exitCode)
		}
		fmt.Println()

	case "container":
		containerID := event.Actor.ID[:12]
		taskID := event.Actor.Attributes["com.docker.swarm.task.id"]
		if taskID != "" {
			taskID = taskID[:12]
		}

		fmt.Printf("[%s] CONTAINER %s (task: %s): %s",
			timestamp,
			containerID,
			taskID,
			event.Action,
		)

		// Show health status for health-related events
		if strings.Contains(string(event.Action), "health") {
			if health := event.Actor.Attributes["health"]; health != "" {
				fmt.Printf(" status=%s", health)
			}
		}
		fmt.Println()

	default:
		// Unknown event type
		fmt.Printf("[%s] %s %s: %s\n",
			timestamp,
			strings.ToUpper(string(event.Type)),
			event.Actor.ID[:12],
			string(event.Action),
		)
	}
}
