package main

import (
	"flag"
	"os"

	"github.com/ameistad/haloy/internal/manager"
)

func main() {
	debugFlag := flag.Bool("debug", false, "Run in debug mode (don't actually send commands to HAProxy)")
	flag.Parse()

	debugEnv := os.Getenv("HALOY_DEBUG") == "true"
	debug := *debugFlag || debugEnv

	manager.RunManager(debug)
}
