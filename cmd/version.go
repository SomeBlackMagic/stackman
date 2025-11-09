package cmd

import (
	"fmt"
)

var (
	version  string = "dev"
	revision string = "000000000000000000000000000000"
)

func printVersion() {
	fmt.Printf("stackman version %s (rev: %s)\n", version, revision)
}
