package haloy

import (
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
			ctx := cmd.Context()

			configFileName, err := appconfigloader.FindConfigFile(*configPath)
			if err != nil {
				ui.Error("%v", err)
				return
			}

			targets, _, err := appconfigloader.Load(ctx, *configPath, nil, true)
			if err != nil {
				ui.Error("Config validation failed: %v", err)
				return
			}

			if showResolvedConfigFlag {
				for _, target := range targets {
					displayResolvedConfig(target.ResolvedAppConfig)
				}
			}

			ui.Success("Config file '%s' is valid!", filepath.Base(configFileName))
		},
	}
	cmd.Flags().BoolVar(&showResolvedConfigFlag, "show-config", false, "Print the resolved config with secrets")
	return cmd
}

func displayResolvedConfig(appConfig config.AppConfig) error {
	var output string

	switch appConfig.Format {
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
		return fmt.Errorf("unsupported format: %s", appConfig.Format)
	}

	ui.Section("Resolved Configuration", []string{output})
	return nil
}
