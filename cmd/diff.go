package cmd

import "github.com/spf13/cobra"

func newDiffCommand() *cobra.Command {
	return newNotImplementedCommand("diff", "Show changes between current state and compose file")
}
