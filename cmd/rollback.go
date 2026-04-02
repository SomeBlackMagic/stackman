package cmd

import "github.com/spf13/cobra"

func newRollbackCommand() *cobra.Command {
	return newNotImplementedCommand("rollback", "Roll back a stack to its previous state")
}
