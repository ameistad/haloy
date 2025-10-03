package haloy

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"

	"github.com/ameistad/haloy/internal/appconfigloader"
	"github.com/ameistad/haloy/internal/config"
	"github.com/ameistad/haloy/internal/ui"
	"github.com/pelletier/go-toml/v2"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

func ValidateAppConfigCmd(configPath *string) *cobra.Command {
	var showResolvedConfigFlag bool

	cmd := &cobra.Command{
		Use:   "validate-config",
		Short: "Validate a haloy config file",
		Long:  "Validate a haloy configuration file.",

		Args: cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			ctx, cancel := context.WithTimeout(context.Background(), defaultContextTimeout)
			defer cancel()

			configFileName, err := appconfigloader.FindConfigFile(*configPath)
			if err != nil {
				ui.Error("%v", err)
				return
			}
			// Just load the file to validate it.
			appConfigs, _, _, format, err := appconfigloader.Load(ctx, *configPath, nil, true)
			if err != nil {
				ui.Error("Config validation failed: %v", err)
				return
			}

			if showResolvedConfigFlag {
				for _, appConfig := range appConfigs {
					displayResolvedConfig(appConfig, format)
				}
			}

			ui.Success("Config file '%s' is valid!", filepath.Base(configFileName))
		},
	}
	cmd.Flags().BoolVar(&showResolvedConfigFlag, "show-config", false, "Print the resolved config with secrets")
	return cmd
}

func displayResolvedConfig(appConfig config.AppConfig, format string) error {
	var output string

	switch format {
	case "json":
		data, err := json.MarshalIndent(appConfig, "", "  ")
		if err != nil {
			return err
		}
		output = string(data)
	case "yaml", "yml":
		data, err := yaml.Marshal(appConfig)
		if err != nil {
			return err
		}
		output = string(data)
	case "toml":
		data, err := toml.Marshal(appConfig)
		if err != nil {
			return err
		}
		output = string(data)
	default:
		return fmt.Errorf("unsupported format: %s", format)
	}

	ui.Section("Resolved Configuration", []string{output})
	return nil
}
