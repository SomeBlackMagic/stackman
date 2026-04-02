package main

import "github.com/SomeBlackMagic/stackman/cmd"

var (
	Version   = "dev"
	Commit    = "unknown"
	BuildDate = "unknown"
)

func init() {
	cmd.Version = Version
	cmd.Commit = Commit
	cmd.BuildDate = BuildDate
}

func main() {
	cmd.Execute()
}
