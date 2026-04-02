package cmd

import "github.com/spf13/cobra"

func newStatusCommand() *cobra.Command {
	return newNotImplementedCommand("status", "Show current status of stack services")
}
