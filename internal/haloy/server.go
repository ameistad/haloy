package haloy

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ameistad/haloy/internal/config"
	"github.com/ameistad/haloy/internal/constants"
	"github.com/ameistad/haloy/internal/helpers"
	"github.com/ameistad/haloy/internal/ui"
	"github.com/joho/godotenv"
	"github.com/spf13/cobra"
)

func ServerCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "server",
		Short: "Manage Haloy servers",
		Long:  "Add, remove, and manage connections to Haloy servers",
	}

	cmd.AddCommand(ServerAddCmd())
	// cmd.AddCommand(ServerDeleteCmd())
	// cmd.AddCommand(ServerListCmd())

	return cmd
}

func ServerAddCmd() *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "add <url> <token>",
		Short: "Add a new Haloy server",
		Args:  cobra.ExactArgs(2),
		Run: func(cmd *cobra.Command, args []string) {
			url, token := args[0], args[1]

			if url == "" {
				ui.Error("URL is required")
				return
			}

			if token == "" {
				ui.Error("Token is required")
				return
			}

			normalizedURL, err := helpers.NormalizeServerURL(url)
			if err != nil {
				ui.Error("Invalid URL: %v", err)
				return
			}

			if err := helpers.IsValidDomain(normalizedURL); err != nil {
				ui.Error("Invalid domain: %v", err)
				return
			}

			configDir, err := config.ConfigDir()
			if err != nil {
				ui.Error("Failed to get config dir: %v", err)
				return
			}

			if err = helpers.EnsureDir(configDir); err != nil {
				ui.Error("Failed to create config dir: %v", err)
				return
			}

			envFile := filepath.Join(configDir, constants.ConfigEnvFileName)

			tokenEnv := generateTokenEnvName(normalizedURL)

			env, err := godotenv.Read(envFile)
			if err != nil {
				if os.IsNotExist(err) {
					// Create empty map if file doesn't exist
					env = make(map[string]string)
				} else {
					ui.Error("Failed to read env file: %v", err)
					return
				}
			}
			env[tokenEnv] = token
			if err := godotenv.Write(env, envFile); err != nil {
				ui.Error("Failed to write env file: %v", err)
				return
			}

			clientConfigPath := filepath.Join(configDir, constants.ClientConfigFileName)
			clientConfig, err := config.LoadClientConfig(clientConfigPath)
			if err != nil {
				ui.Error("Failed to load client config: %v", err)
				return
			}

			clientConfig.AddServer(normalizedURL, tokenEnv, force)

			if err := config.SaveClientConfig(clientConfig, clientConfigPath); err != nil {
				ui.Error("Failed to save client config: %v", err)
				return
			}

			ui.Success("Server %s added successfully", normalizedURL)
			ui.Info("API token stored as: %s", tokenEnv)
		},
	}

	cmd.Flags().BoolVarP(&force, "force", "f", false, "Force overwrite if server already exists")

	return cmd
}

func generateTokenEnvName(url string) string {
	return fmt.Sprintf("HALOY_TOKEN_%s", strings.ToUpper(helpers.SanitizeString(url)))
}
