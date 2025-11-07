package cmd

import (
	"fmt"
	"os"
)

// ExecuteRollback runs the rollback command (stub)
func ExecuteRollback(args []string) {
	fmt.Fprintln(os.Stderr, "rollback command not yet implemented")
	os.Exit(1)
}

// ExecuteDiff runs the diff command (stub)
func ExecuteDiff(args []string) {
	fmt.Fprintln(os.Stderr, "diff command not yet implemented")
	os.Exit(1)
}

// ExecuteStatus runs the status command (stub)
func ExecuteStatus(args []string) {
	fmt.Fprintln(os.Stderr, "status command not yet implemented")
	os.Exit(1)
}
