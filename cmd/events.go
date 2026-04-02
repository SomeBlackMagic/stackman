package cmd

import "github.com/spf13/cobra"

func newEventsCommand() *cobra.Command {
	return newNotImplementedCommand("events", "Show Docker events for a stack")
}
