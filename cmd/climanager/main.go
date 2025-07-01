package main

import (
	"fmt"
	"os"

	"github.com/ameistad/haloy/internal/climanager"
)

func main() {
	rootCmd := climanager.NewRootCmd()
	if err := rootCmd.Execute(); err != nil {
		// Print error once, then exit
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
