package haloy

import (
	"path/filepath"

	"github.com/ameistad/haloy/internal/config"
	"github.com/ameistad/haloy/internal/ui"
	"github.com/spf13/cobra"
)

func ValidateAppConfigCmd(configPath *string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "validate-config",
		Short: "Validate a haloy config file",
		Long:  "Validate a haloy configuration file.",

		Args: cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			// Get the filename of the config
			configFileName, err := config.FindConfigFile(*configPath)
			if err != nil {
				ui.Error("%v", err)
				return
			}
			// Just load the file to validate it.
			_, _, err = config.LoadAppConfig(*configPath)
			if err != nil {
				ui.Error("Config validation failed: %v", err)
				return
			}

			ui.Success("Config file '%s' is valid!", filepath.Base(configFileName))
		},
	}

	return cmd
}
