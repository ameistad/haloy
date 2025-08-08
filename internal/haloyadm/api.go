package haloyadm

import (
	"context"
	"fmt"
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
		Run: func(cmd *cobra.Command, args []string) {
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

			token, exists := env[constants.EnvVarAPIToken]
			if !exists || token == "" {
				ui.Error("API token not found in %s", envFile)
				return
			}

			if raw {
				fmt.Print(token)
			} else {
				ui.Info("API token: %s\n", token)
			}
		},
	}
	cmd.Flags().BoolVar(&raw, "raw", false, "Output only the token value")
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

	return cmd
}
