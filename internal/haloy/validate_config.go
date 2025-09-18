package haloy

import (
	"path/filepath"

	"github.com/ameistad/haloy/internal/config"
	"github.com/ameistad/haloy/internal/ui"
	"github.com/spf13/cobra"
)

// NewValidateCmd creates a new validate command
func ValidateAppConfigCmd(configPath *string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "validate-config",
		Short: "Validate a haloy config file",
		Long:  "Validate a haloy configuration file.",

		Args: cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			// Just load the file to validate it.
			_, _, err := config.LoadAppConfig(*configPath)
			if err != nil {
				ui.Error("Config validation failed: %v", err)
				return
			}

			fileName := filepath.Base(filepath.Clean(*configPath))

			ui.Success("Config file '%s' is valid!", fileName)
		},
	}

	return cmd
}
