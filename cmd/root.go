package cmd

import (
	"fmt"
	"os"
)

// Execute runs the root command
func Execute() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	command := os.Args[1]
	args := os.Args[2:]

	switch command {
	case "apply":
		ExecuteApply(args)
	case "rollback":
		ExecuteRollback(args)
	case "diff":
		ExecuteDiff(args)
	case "status":
		ExecuteStatus(args)
	case "logs":
		ExecuteLogs(args)
	case "events":
		ExecuteEvents(args)
	case "version", "-v", "--version":
		printVersion()
	case "help", "-h", "--help":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n\n", command)
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	usage := `stackman - Docker Swarm stack management tool

Usage:
  stackman <command> [flags]

Available Commands:
  apply       Deploy or update a stack
  rollback    Rollback stack to previous state
  diff        Show deployment plan without applying
  status      Show current stack status
  logs        Show logs for stack services
  events      Show events for stack services
  version     Show version information
  help        Show this help message

Flags:
  -h, --help      Show help for command
  --docker-host   Docker daemon socket (default: $DOCKER_HOST or unix:///var/run/docker.sock)
  --tls           Use TLS; implied by --tlsverify
  --cert-path     Path to TLS certificates (default: $DOCKER_CERT_PATH)
  --debug         Enable debug logging
  --json          Output in JSON format

Use "stackman <command> --help" for more information about a command.
`
	fmt.Fprint(os.Stderr, usage)
}
