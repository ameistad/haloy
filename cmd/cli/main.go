package main

import (
	"fmt"
	"os"

	"github.com/ameistad/haloy/internal/cli/commands"
)

func main() {
	rootCmd := commands.NewRootCmd()
	if err := rootCmd.Execute(); err != nil {
		// Print error once, then exit
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
