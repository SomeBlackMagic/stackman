package cmd

import (
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"
)

func newVersionCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Run: func(command *cobra.Command, _ []string) {
			ExecuteVersionTo(command.OutOrStdout())
		},
	}
}

func ExecuteVersion() {
	ExecuteVersionTo(os.Stdout)
}

func ExecuteVersionTo(w io.Writer) {
	fmt.Fprintf(w, "stackman %s\n  commit: %s\n  built:  %s\n", Version, Commit, BuildDate)
}
