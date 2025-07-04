package haloyadm

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
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
		Long:  "Stop the haloy services, including HAProxy and haloy-manager.",
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {

			ctx, cancel := context.WithTimeout(context.Background(), stopTimeout)
			defer cancel()

			if err := stopContainer(ctx, config.HaloyManagerContainerName); err != nil {
				ui.Error("Failed to stop haloy-manager: %v", err)
				return
			}

			if err := stopContainer(ctx, config.HAProxyContainerName); err != nil {
				ui.Error("Failed to stop HAProxy: %v", err)
				return
			}

			ui.Success("Haloy services stopped successfully")
		},
	}
	return cmd
}

func stopContainer(ctx context.Context, containerName string) error {
	cmd := exec.CommandContext(ctx, "docker", "rm", "-f", containerName)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if stderr.Len() > 0 {
			return fmt.Errorf("failed to stop and remove %s: %s", containerName, stderr.String())
		}
		return fmt.Errorf("failed to stop and remove %s: %w", containerName, err)
	}

	return nil
}
