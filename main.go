package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/docker/docker/client"

	"stackwait/compose"
	"stackwait/deployer"
	"stackwait/monitor"
)

// -----------------------------------
var (
	version  string = "dev"
	revision string = "000000000000000000000000000000"
)

//-----------------------------------

func main() {
	if len(os.Args) < 3 {
		log.Fatalf("Usage: %s <stack-name> <docker-compose.yml> [health-timeout-minutes] [max-failed-tasks]", os.Args[0])
	}

	log.Printf("Start Docker Stack Wait version=%s revision=%s", version, revision)

	stackName := os.Args[1]
	composeFile := os.Args[2]

	// Get health check timeout (default 1 minute)
	healthTimeout := 1 * time.Minute
	if len(os.Args) >= 4 {
		minutes, err := strconv.Atoi(os.Args[3])
		if err != nil {
			log.Printf("Invalid timeout value, using default: 1 minute")
		} else {
			healthTimeout = time.Duration(minutes) * time.Minute
			log.Printf("Health check timeout set to: %d minutes", minutes)
		}
	}

	// Check environment variable override
	if envTimeout := os.Getenv("HEALTH_TIMEOUT_MINUTES"); envTimeout != "" {
		minutes, err := strconv.Atoi(envTimeout)
		if err == nil {
			healthTimeout = time.Duration(minutes) * time.Minute
			log.Printf("Health check timeout from env: %d minutes", minutes)
		}
	}

	// Get max failed tasks count (default 3)
	maxFailedTaskCount := 3
	if len(os.Args) >= 5 {
		count, err := strconv.Atoi(os.Args[4])
		if err != nil {
			log.Printf("Invalid max-failed-tasks value, using default: 3")
		} else {
			maxFailedTaskCount = count
			log.Printf("Max failed tasks count set to: %d", count)
		}
	}

	// Check environment variable override
	if envMaxFailed := os.Getenv("MAX_FAILED_TASKS"); envMaxFailed != "" {
		count, err := strconv.Atoi(envMaxFailed)
		if err == nil {
			maxFailedTaskCount = count
			log.Printf("Max failed tasks count from env: %d", count)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), healthTimeout+5*time.Minute)
	defer cancel()

	// Setup signal handling for graceful shutdown and rollback
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM, syscall.SIGINT)

	// Initialize Docker client
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		log.Fatalf("docker client init: %v", err)
	}
	defer cli.Close()

	// Parse compose file
	log.Printf("Parsing compose file: %s", composeFile)
	composeSpec, err := compose.ParseComposeFile(composeFile)
	if err != nil {
		log.Fatalf("failed to parse compose file: %v", err)
	}

	// Create deployer
	stackDeployer := deployer.NewStackDeployer(cli, stackName, maxFailedTaskCount)

	// Create snapshot before deployment for rollback
	log.Println("Creating snapshot of current stack state...")
	snapshot, err := stackDeployer.CreateSnapshot(ctx)
	if err != nil {
		log.Printf("Warning: failed to create snapshot: %v", err)
		log.Println("Continuing without rollback capability")
	}

	// Track deployment state for signal handler
	deploymentComplete := make(chan bool, 1)

	// Handle signals in background
	go func() {
		sig := <-sigChan
		log.Printf("Received signal: %v", sig)

		select {
		case <-deploymentComplete:
			// Deployment already completed, just exit
			log.Println("Deployment already completed, exiting...")
			os.Exit(0)
		default:
			// Deployment in progress, rollback
			log.Println("Deployment interrupted, initiating rollback...")
			rollback(context.Background(), stackDeployer, snapshot)
			os.Exit(130) // Standard exit code for SIGINT
		}
	}()

	// Start log streaming BEFORE deployment to catch all container events
	logStreamer := monitor.NewLogStreamer(cli, stackName)
	go logStreamer.StreamLogs(ctx)

	// Start event streaming BEFORE deployment
	eventStreamer := monitor.NewEventStreamer(cli, stackName)
	go eventStreamer.StreamEvents(ctx)

	// Start health log streaming BEFORE deployment
	healthLogStreamer := monitor.NewHealthLogStreamer(cli, stackName)
	go healthLogStreamer.StreamHealthLogs(ctx)

	// Deploy stack
	log.Printf("Deploying stack: %s", stackName)
	if err := stackDeployer.Deploy(ctx, composeSpec); err != nil {
		log.Fatalf("failed to deploy stack: %v", err)
	}

	fmt.Println("Stack deployed successfully. Starting health checks...")

	// Wait for services to be ready
	healthMonitor := monitor.NewHealthMonitor(cli, stackName, maxFailedTaskCount)

	// Create context with health check timeout
	healthCtx, healthCancel := context.WithTimeout(ctx, healthTimeout)
	defer healthCancel()

	// First wait for service tasks to start
	log.Println("Waiting for service tasks to start...")
	if err := healthMonitor.WaitServicesReady(healthCtx); err != nil {
		log.Printf("ERROR: %v", err)
		rollback(ctx, stackDeployer, snapshot)
		os.Exit(1)
	}

	// Then wait for containers to become healthy
	if !healthMonitor.WaitHealthy(healthCtx) {
		fmt.Println("ERROR: Services failed healthcheck or didn't start in time.")
		rollback(ctx, stackDeployer, snapshot)
		os.Exit(1)
	}

	fmt.Println("All containers healthy.")

	// Mark deployment as successful
	deploymentComplete <- true

	cancel()
}

// --- Rollback ---
func rollback(ctx context.Context, stackDeployer *deployer.StackDeployer, snapshot *deployer.StackSnapshot) {
	if snapshot == nil || len(snapshot.Services) == 0 {
		log.Println("No snapshot available, cannot rollback")
		return
	}

	fmt.Println("Starting rollback to previous state...")

	// Create new context with timeout for rollback
	rollbackCtx, rollbackCancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer rollbackCancel()

	if err := stackDeployer.Rollback(rollbackCtx, snapshot); err != nil {
		log.Printf("Rollback failed: %v", err)
		log.Println("Manual intervention may be required")
		return
	}

	fmt.Println("Rollback completed successfully")
}
