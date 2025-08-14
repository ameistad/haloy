package haloy

import (
	"github.com/ameistad/haloy/internal/config"
	"github.com/ameistad/haloy/internal/ui"
	"github.com/spf13/cobra"
)

// NewValidateCmd creates a new validate command
func ValidateAppConfigCmd() *cobra.Command {
	var configPath string

	cmd := &cobra.Command{
		Use:   "validate-config [config-path]",
		Short: "Validate a haloy config file",
		Long: `Validate a haloy configuration file.

The path can be:
- A directory containing haloy.json, haloy.yaml, haloy.yml, or haloy.toml
- A full path to a config file with supported extension (.json, .yaml, .yml, .toml)
- A relative path to either of the above

If no path is provided, the current directory is used.`,
		Args: cobra.MaximumNArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			// Use argument if provided, otherwise use flag, otherwise use current directory
			if len(args) > 0 {
				configPath = args[0]
			} else if configPath == "" {
				configPath = "."
			}

			// Find the actual config file
			foundConfigFile, err := config.FindConfigFile(configPath)
			if err != nil {
				ui.Error("Could not find config file: %v", err)
				return
			}

			// Load and validate the config
			_, _, err = config.LoadAppConfig(configPath)
			if err != nil {
				ui.Error("Config validation failed: %v", err)
				return
			}

			ui.Success("Config file '%s' is valid!", foundConfigFile)
		},
	}

	cmd.Flags().StringVarP(&configPath, "config", "c", "", "Path to config file or directory")

	return cmd
}
