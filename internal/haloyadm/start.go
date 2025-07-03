package haloyadm

import (
	"context"
	"time"

	"github.com/ameistad/haloy/internal/ui"
	"github.com/spf13/cobra"
)

const (
	startTimeout = 5 * time.Minute
)

func StartCmd() *cobra.Command {
	var devMode bool
	cmd := &cobra.Command{
		Use:   "start",
		Short: "Start the haloy services",
		Long:  "Start the haloy services, including HAProxy and haloy-manager.",
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {

			ctx, cancel := context.WithTimeout(context.Background(), startTimeout)
			defer cancel()

			if err := startServices(ctx, devMode); err != nil {
				ui.Error("%s", err)
				return
			}
		},
	}
	cmd.Flags().BoolVar(&devMode, "dev", false, "Start in development mode using the local haloy-manager image")
	return cmd
}
