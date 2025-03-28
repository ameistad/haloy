package commands

import (
	"fmt"

	"github.com/ameistad/haloy/internal/config"
	"github.com/spf13/cobra"
)

// NewValidateCmd creates a new validate command
func ValidateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "validate",
		Short:        "Validate the config file",
		SilenceUsage: true, // Don't show usage on error
		RunE: func(cmd *cobra.Command, args []string) error {
			confFilePath, err := config.ConfigFilePath()
			if err != nil {
				return fmt.Errorf("couldn't determine config file path: %w", err)
			}

			_, err = config.LoadAndValidateConfig(confFilePath)
			if err != nil {
				return fmt.Errorf("failed to load config from '%s': %w", confFilePath, err)
			}

			fmt.Println("Config file is valid!")
			return nil
		},
	}

	return cmd
}
