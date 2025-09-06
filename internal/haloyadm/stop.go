package haloyadm

import (
	"context"
	"time"

	"github.com/ameistad/haloy/internal/config"
	"github.com/ameistad/haloy/internal/ui"
	"github.com/spf13/cobra"
)

const (
	stopTimeout = 5 * time.Minute
)

func StopCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "stop",
		Short: "Stop the haloy services",
		Long:  "Stop the haloy services, including HAProxy and haloyd.",
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {

			ctx, cancel := context.WithTimeout(context.Background(), stopTimeout)
			defer cancel()

			if err := stopContainer(ctx, config.HaloydLabelRole); err != nil {
				ui.Error("Failed to stop haloyd: %v", err)
				return
			}

			if err := stopContainer(ctx, config.HAProxyLabelRole); err != nil {
				ui.Error("Failed to stop HAProxy: %v", err)
				return
			}

			ui.Success("Haloy services stopped successfully")
		},
	}
	return cmd
}
