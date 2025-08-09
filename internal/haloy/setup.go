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
	var sshKey string
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

			token, err := fetchTokenViaSSH(host, port, sshKey)
			if err != nil {
				ui.Error("Failed to fetch token: %v", err)
				return
			}

			if token == "" {
				ui.Error("No token returned from server")
				return
			}

			if err := saveTokenLocally(token); err != nil {
				ui.Error("Failed to save token locally: %v", err)
				return
			}

			ui.Success("✅ Successfully configured haloy client!")
			ui.Info("You can now deploy to this server using: haloy deploy")
		},
	}

	cmd.Flags().StringVarP(&sshKey, "identity", "i", "", "SSH private key file")
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

func fetchTokenViaSSH(host string, port int, keyFile string) (string, error) {
	args := []string{}
	if port != 22 {
		args = append(args, "-p", fmt.Sprint(port))
	}
	if keyFile != "" {
		args = append(args, "-i", keyFile)
	}
	args = append(args, host, "haloyadm api token --raw")

	cmd := exec.Command("ssh", args...)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("ssh command failed: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

func saveTokenLocally(token string) error {
	configDir, err := config.ConfigDir()
	if err != nil {
		return fmt.Errorf("failed to get user config dir: %w", err)
	}

	// Ensure config directory exists with private permissions
	if err := os.MkdirAll(configDir, constants.ModeDirPrivate); err != nil {
		return fmt.Errorf("failed to create config dir: %w", err)
	}
	_ = os.Chmod(configDir, constants.ModeDirPrivate)

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
