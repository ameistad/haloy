package commands

import (
	"github.com/ameistad/haloy/internal/config"
	"github.com/ameistad/haloy/internal/ui"
	"github.com/spf13/cobra"
)

// NewValidateCmd creates a new validate command
func ValidateConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "validate-config",
		Short: "Validate the config file",
		Run: func(cmd *cobra.Command, args []string) {
			confFilePath, err := config.ConfigFilePath()
			if err != nil {
				ui.Error("couldn't determine config file path: %v", err)
				return
			}

			_, err = config.LoadAndValidateConfig(confFilePath)
			if err != nil {
				ui.Error("Config file found at '%s' is not valid: %v", confFilePath, err)
				return
			}

			ui.Success("Config file '%s' is valid!\n", confFilePath)
		},
	}

	return cmd
}
