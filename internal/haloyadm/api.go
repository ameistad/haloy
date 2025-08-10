package haloyadm

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/ameistad/haloy/internal/config"
	"github.com/ameistad/haloy/internal/constants"
	"github.com/ameistad/haloy/internal/ui"
	"github.com/joho/godotenv"
	"github.com/spf13/cobra"
)

func APITokenCmd() *cobra.Command {
	var raw bool
	cmd := &cobra.Command{
		Use:   "token",
		Short: "Reveal API token",
		RunE: func(cmd *cobra.Command, args []string) error {
			configDir, err := config.ConfigDir()
			if err != nil {
				if raw {
					fmt.Fprintln(os.Stderr, err)
				} else {
					ui.Error("Failed to determine config directory: %v\n", err)
				}
				return err
			}

			envFile := filepath.Join(configDir, constants.ConfigEnvFileName)
			env, err := godotenv.Read(envFile)
			if err != nil {
				if raw {
					fmt.Fprintln(os.Stderr, err)
				} else {
					ui.Error("Failed to read environment variables from %s: %v", envFile, err)
				}
				return err
			}

			token, exists := env[constants.EnvVarAPIToken]
			if !exists || token == "" {
				err := fmt.Errorf("API token not found in %s", envFile)
				if raw {
					fmt.Fprintln(os.Stderr, err)
				} else {
					ui.Error("API token not found in %s", envFile)
				}
				return err
			}

			if raw {
				fmt.Print(token)
			} else {
				ui.Info("API token: %s\n", token)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&raw, "raw", false, "Output only the token value")
	return cmd
}

func APIURLCmd() *cobra.Command {
	var raw bool
	cmd := &cobra.Command{
		Use:   "url",
		Short: "Show API URL",
		RunE: func(cmd *cobra.Command, args []string) error {
			configDir, err := config.ConfigDir()
			if err != nil {
				if raw {
					fmt.Fprintln(os.Stderr, err)
				} else {
					ui.Error("Failed to determine config directory: %v\n", err)
				}
				return err
			}

			configFilePath := filepath.Join(configDir, constants.ManagerConfigFileName)
			managerConfig, err := config.LoadManagerConfig(configFilePath)
			if err != nil {
				if raw {
					fmt.Fprintln(os.Stderr, err)
				} else {
					ui.Error("Failed to load configuration file: %v", err)
				}
				return err
			}

			if managerConfig == nil || managerConfig.API.Domain == "" {
				err := fmt.Errorf("API URL not found")
				if raw {
					fmt.Fprintln(os.Stderr, err)
				} else {
					ui.Error("API URL not found in %s", configFilePath)
				}
				return err
			}

			if raw {
				fmt.Print(managerConfig.API.Domain)
			} else {
				ui.Info("API URL: %s\n", managerConfig.API.Domain)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&raw, "raw", false, "Output only the URL value")
	return cmd
}

const (
	newTokenTimeout = 1 * time.Minute
)

func APINewTokenCmd() *cobra.Command {
	var devMode bool
	var debug bool
	cmd := &cobra.Command{
		Use:   "generate-token",
		Short: "Generate a new API token and restart the haloy-manager",
		Run: func(cmd *cobra.Command, args []string) {
			ctx, cancel := context.WithTimeout(context.Background(), newTokenTimeout)
			defer cancel()

			token, err := generateAPIToken()
			if err != nil {
				ui.Error("Failed to generate API token: %v\n", err)
				return
			}
			dataDir, err := config.DataDir()
			if err != nil {
				ui.Error("Failed to determine data directory: %v\n", err)
				return
			}
			configDir, err := config.ConfigDir()
			if err != nil {
				ui.Error("Failed to determine config directory: %v\n", err)
				return
			}

			envFile := filepath.Join(configDir, constants.ConfigEnvFileName)
			env, err := godotenv.Read(envFile)
			if err != nil {
				ui.Error("Failed to read environment variables from %s: %v", envFile, err)
				return
			}
			env[constants.EnvVarAPIToken] = token
			if err := godotenv.Write(env, envFile); err != nil {
				ui.Error("Failed to write environment variables to %s: %v", envFile, err)
				return
			}

			// Restart haloy-manager
			if err := stopContainer(ctx, config.ManagerLabelRole); err != nil {
				ui.Error("Failed to stop haloy-manager container: %v", err)
				return
			}
			if err := startHaloyManager(ctx, dataDir, configDir, devMode, debug); err != nil {
				ui.Error("Failed to restart haloy-manager: %v", err)
				return
			}

			ui.Success("Generated new API token and restarted haloy-manager")
			ui.Info("New API token: %s\n", token)
		},
	}
	cmd.Flags().BoolVar(&devMode, "dev", false, "Restart in development mode using the local haloy-manager image")
	cmd.Flags().BoolVar(&debug, "debug", false, "Restart haloy-manager in debug mode")
	return cmd
}

func APICmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "api",
		Short: "API related commands",
	}

	cmd.AddCommand(APITokenCmd())
	cmd.AddCommand(APINewTokenCmd())
	cmd.AddCommand(APIURLCmd())

	return cmd
}
