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

			rawAppConfig, format, err := appconfigloader.LoadRawAppConfig(*configPath)
			if err != nil {
				ui.Error("Unable to load config file from %s: %v", *configPath, err)
				return
			}

			rawAppConfig.Normalize()

			if err := rawAppConfig.Validate(format); err != nil {
				ui.Error("Not a valid config: %v", err)
				return
			}

			if showResolvedConfigFlag {
				rawAppConfig.Format = format

				resolvedAppConfig, err := appconfigloader.ResolveSecrets(ctx, rawAppConfig)
				if err != nil {
					ui.Error("Unable to resolve secrets: %v", err)
					return
				}
				resolvedTargets, err := appconfigloader.ResolveTargets(resolvedAppConfig)
				if err != nil {
					ui.Error("Unable to resolve targets for config: %v", err)
					return
				}
				for _, resolvedTarget := range resolvedTargets {
					if err := displayResolvedConfig(resolvedTarget); err != nil {
						ui.Error("Failed to display resolved config: %v", err)
					}
				}

			}

			ui.Success("Config file '%s' is valid!", filepath.Base(configFileName))
		},
	}
	cmd.Flags().BoolVar(&showResolvedConfigFlag, "show-resolved-config", false, "Print the resolved configuration with all fields and secrets resolved and visible in plain text (WARNING: sensitive data will be displayed)")
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

	targetName := appConfig.TargetName
	if targetName == "" {
		targetName = appConfig.Name
	}

	ui.Section(fmt.Sprintf("Resolved Configuration for %s", targetName), []string{output})
	return nil
}
