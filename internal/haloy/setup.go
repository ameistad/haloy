package haloy

import (
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/ameistad/haloy/internal/config"
	"github.com/ameistad/haloy/internal/constants"
	"github.com/ameistad/haloy/internal/ui"
	"github.com/joho/godotenv"
	"github.com/spf13/cobra"
)

func SetupSSHCmd() *cobra.Command {
	var keyFile string
	var port int

	cmd := &cobra.Command{
		Use:   "ssh [user@]host",
		Short: "Setup haloy by fetching API token from remote server via SSH",
		Long: `Connect to the haloy server via SSH, retrieve the API token, and configure the local client.

Examples:
  haloy setup ssh user@host
  haloy setup ssh user@host -p 2222 -i ~/.ssh/mykey user@host
  haloy setup ssh -i ~/.ssh/id_ed25519 hermes
`,
		Args: cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			host := args[0]

			ui.Info("Connecting to %s...", host)

			token, err := executeRemoteSSH(host, port, keyFile, "haloyadm api token --raw")
			if err != nil {
				ui.Error("Failed to fetch token: %v", err)
				return
			}

			if token == "" {
				ui.Error("No token returned from server")
				return
			}

			configDir, err := config.ConfigDir()
			if err != nil {
				ui.Error("Failed to get user config directory: %v", err)
				return
			}

			if err := os.MkdirAll(configDir, constants.ModeDirPrivate); err != nil {
				ui.Error("Failed to create config directory: %v", err)
				return
			}

			if err := saveTokenLocally(configDir, token); err != nil {
				ui.Error("Failed to save token locally: %v", err)
				return
			}

			url, err := executeRemoteSSH(host, port, keyFile, "haloyadm api url --raw")
			if err != nil {
				ui.Warn("Failed to fetch API URL: %v", err)
			}
			if url != "" {
				if err := saveAPIURL(configDir, url); err != nil {
					ui.Error("Failed to save API URL: %v", err)
					return
				}
			}

			ui.Success("Successfully configured haloy client!")
			ui.Info("You can now deploy to this server using: haloy deploy")
		},
	}

	cmd.Flags().StringVarP(&keyFile, "identity", "i", "", "SSH private key file")
	cmd.Flags().IntVarP(&port, "port", "p", 22, "SSH port")

	return cmd
}

func SetupCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Setup and configure haloy client",
	}

	cmd.AddCommand(SetupSSHCmd())
	return cmd
}

func saveAPIURL(configDir, url string) error {
	configFilePath := filepath.Join(configDir, constants.ManagerConfigFileName)
	managerConfig, err := config.LoadManagerConfig(configFilePath)
	if err != nil {
		return fmt.Errorf("failed to load manager config: %w", err)
	}

	if managerConfig == nil {
		managerConfig = &config.ManagerConfig{}
	}

	managerConfig.API.Domain = url

	if err := managerConfig.Save(configFilePath); err != nil {
		return fmt.Errorf("failed to save manager config: %w", err)
	}

	return nil

}

func saveTokenLocally(configDir, token string) error {

	envFile := filepath.Join(configDir, constants.ConfigEnvFileName)

	env := map[string]string{}
	if _, statErr := os.Stat(envFile); statErr == nil {
		if existing, readErr := godotenv.Read(envFile); readErr == nil {
			maps.Copy(env, existing)
		} else {
			ui.Warn("Could not read existing %s file: %v", constants.ConfigEnvFileName, readErr)
		}
	}

	env[constants.EnvVarAPIToken] = token

	if err := godotenv.Write(env, envFile); err != nil {
		return fmt.Errorf("failed to write .env file: %w", err)
	}
	_ = os.Chmod(envFile, constants.ModeFileSecret)

	return nil
}

func executeRemoteSSH(host string, port int, keyFile string, remoteCmd string) (string, error) {
	args := []string{}
	if port != 22 {
		args = append(args, "-p", fmt.Sprint(port))
	}
	if keyFile != "" {
		args = append(args, "-i", keyFile)
	}
	args = append(args, host, fmt.Sprintf("bash -lc '%s'", remoteCmd))

	cmd := exec.Command("ssh", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("ssh command failed: %w; output: %s", err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}
