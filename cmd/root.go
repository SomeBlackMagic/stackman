package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var (
	Version   = "dev"
	Commit    = "unknown"
	BuildDate = "unknown"
)

type rootOptions struct {
	LogLevel string
	NoColor  bool
	Output   string
}

func NewRootCommand() *cobra.Command {
	opts := &rootOptions{}

	command := &cobra.Command{
		Use:          "stackman",
		Short:        "Manage Docker Swarm stacks",
		SilenceUsage: true,
	}
	command.CompletionOptions.DisableDefaultCmd = true

	command.PersistentFlags().StringVar(&opts.LogLevel, "log-level", "info", "Log level: debug, info, warn, error")
	command.PersistentFlags().BoolVar(&opts.NoColor, "no-color", false, "Disable colored output")
	command.PersistentFlags().StringVar(&opts.Output, "output", "text", "Output format: text, json")

	command.AddCommand(
		newApplyCommand(),
		newRollbackCommand(),
		newLogsCommand(),
		newEventsCommand(),
		newDiffCommand(),
		newStatusCommand(),
		newVersionCommand(),
	)

	return command
}

func Execute() {
	if err := NewRootCommand().Execute(); err != nil {
		os.Exit(1)
	}
}

func newNotImplementedCommand(use, short string) *cobra.Command {
	return &cobra.Command{
		Use:   use,
		Short: short,
		RunE: func(_ *cobra.Command, _ []string) error {
			return fmt.Errorf("%s: not implemented yet", use)
		},
	}
}
