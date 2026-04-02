package cmd

import (
	"errors"
	"fmt"
	"io"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type ApplyArgs struct {
	StackName   string
	ComposeFile string
	Timeout     string
	Prune       bool
	NoLogs      bool
}

func newApplyCommand() *cobra.Command {
	applyArgs := &ApplyArgs{}

	command := &cobra.Command{
		Use:   "apply",
		Short: "Deploy a stack to Docker Swarm",
		RunE: func(_ *cobra.Command, _ []string) error {
			if err := validateApplyArgs(applyArgs); err != nil {
				return err
			}

			return fmt.Errorf("apply: not implemented yet")
		},
	}

	bindApplyFlags(command.Flags(), applyArgs)
	return command
}

func parseApplyArgs(args []string) (*ApplyArgs, error) {
	fs := pflag.NewFlagSet("apply", pflag.ContinueOnError)
	fs.SetOutput(io.Discard)

	applyArgs := &ApplyArgs{}
	bindApplyFlags(fs, applyArgs)

	if err := fs.Parse(args); err != nil {
		return nil, fmt.Errorf("parse apply flags: %w", err)
	}
	if err := validateApplyArgs(applyArgs); err != nil {
		return nil, err
	}

	return applyArgs, nil
}

func bindApplyFlags(fs *pflag.FlagSet, applyArgs *ApplyArgs) {
	fs.StringVarP(&applyArgs.StackName, "name", "n", "", "Stack name (required)")
	fs.StringVarP(&applyArgs.ComposeFile, "compose-file", "f", "", "Path to compose file (required)")
	fs.StringVar(&applyArgs.Timeout, "timeout", "5m", "Deployment timeout")
	fs.BoolVar(&applyArgs.Prune, "prune", false, "Remove orphaned resources")
	fs.BoolVar(&applyArgs.NoLogs, "no-logs", false, "Disable log streaming")
}

func validateApplyArgs(applyArgs *ApplyArgs) error {
	if applyArgs.StackName == "" {
		return errors.New("required flag --name not set")
	}
	if applyArgs.ComposeFile == "" {
		return errors.New("required flag --compose-file not set")
	}

	return nil
}
