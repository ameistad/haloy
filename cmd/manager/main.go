package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/ameistad/haloy/internal/manager"
)

func main() {
	// Parse command line flags
	dryRunFlag := flag.Bool("dry-run", false, "Run in dry-run mode (don't actually send commands to HAProxy)")
	flag.Parse()

	dryRunEnv := os.Getenv("DRY_RUN") == "true"
	dryRun := *dryRunFlag || dryRunEnv

	if dryRun {
		fmt.Println("========================")
		fmt.Println("STARTING IN DRY RUN MODE")
		fmt.Println("No changes will be made to HAProxy configuration")
		fmt.Println("========================")
	}
	manager.RunManager(dryRun)
}
