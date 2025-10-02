package haloy

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"

	"github.com/ameistad/haloy/internal/config"
	"github.com/ameistad/haloy/internal/configresolver"
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
			configFileName, err := config.FindConfigFile(*configPath)
			if err != nil {
				ui.Error("%v", err)
				return
			}
			// Just load the file to validate it.
			appConfig, format, err := config.LoadAppConfig(*configPath)
			if err != nil {
				ui.Error("Config validation failed: %v", err)
				return
			}

			ctx, cancel := context.WithTimeout(context.Background(), defaultContextTimeout)
			defer cancel()

			resolvedAppConfig, err := configresolver.Resolve(ctx, &appConfig, format)
			if err != nil {
				ui.Error("Config validation failed: %v", err)
				return
			}

			if showResolvedConfigFlag {
				displayResolvedConfig(resolvedAppConfig, format)
			}

			ui.Success("Config file '%s' is valid!", filepath.Base(configFileName))
		},
	}
	cmd.Flags().BoolVar(&showResolvedConfigFlag, "show-config", false, "Print the resolved config with secrets")
	return cmd
}

func displayResolvedConfig(appConfig *config.AppConfig, format string) error {
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
