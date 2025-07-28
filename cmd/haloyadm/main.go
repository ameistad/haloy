package main

import (
	"fmt"
	"os"

	"github.com/ameistad/haloy/internal/haloyadm"
)

func main() {
	rootCmd := haloyadm.NewRootCmd()
	if err := rootCmd.Execute(); err != nil {
		// Print error once, then exit
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
