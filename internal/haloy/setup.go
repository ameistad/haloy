package haloy

import (
	"errors"
	"fmt"
	"maps"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ameistad/haloy/internal/config"
	"github.com/ameistad/haloy/internal/constants"
	"github.com/ameistad/haloy/internal/ui"
	"github.com/joho/godotenv"
	"github.com/spf13/cobra"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
	"golang.org/x/crypto/ssh/knownhosts"
)

func SetupSSHCmd() *cobra.Command {
	var sshKey string
	var port int

	cmd := &cobra.Command{
		Use:   "ssh [user@]host",
		Short: "Setup haloy by fetching API token from remote server via SSH",
		Long: `Connect to the haloy server via SSH, retrieve the API token, and configure the local client.

Examples:
  haloy setup ssh root@server.com
  haloy setup ssh user@192.168.1.100
  haloy setup ssh -p 2222 -i ~/.ssh/mykey user@server.com`,
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

func fetchTokenViaSSH(hostPort string, port int, keyFile string) (string, error) {
	user, host := parseSSHTarget(hostPort)

	config, cleanup, err := buildSSHConfig(user, keyFile)
	if err != nil {
		return "", err
	}
	defer cleanup()

	addr := net.JoinHostPort(host, fmt.Sprint(port))
	client, err := ssh.Dial("tcp", addr, config)
	if err != nil {
		return "", fmt.Errorf("SSH connection failed: %w", err)
	}
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		return "", err
	}
	defer session.Close()

	// Use a login shell so PATH is correct for non-interactive sessions
	out, err := session.CombinedOutput("bash -lc 'haloyadm api token --raw'")
	if err != nil {
		return "", fmt.Errorf("remote command failed: %w; output: %s", err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}

// Returns config and a cleanup func to close the SSH agent connection if used.
func buildSSHConfig(userName, keyFile string) (*ssh.ClientConfig, func(), error) {
	var (
		authMethods []ssh.AuthMethod
		cleanup     = func() {}
	)

	// SSH agent (close the socket when done)
	if sock := os.Getenv("SSH_AUTH_SOCK"); sock != "" {
		if conn, err := net.Dial("unix", sock); err == nil {
			ag := agent.NewClient(conn)
			authMethods = append(authMethods, ssh.PublicKeysCallback(ag.Signers))
			cleanup = func() { _ = conn.Close() }
		}
	}

	// Specific key file
	if keyFile != "" {
		if signer, err := loadPrivateKey(keyFile); err == nil {
			authMethods = append(authMethods, ssh.PublicKeys(signer))
		}
	}

	// Default keys
	homeDir, _ := os.UserHomeDir()
	for _, p := range []string{
		filepath.Join(homeDir, ".ssh", "id_ed25519"),
		filepath.Join(homeDir, ".ssh", "id_rsa"),
		filepath.Join(homeDir, ".ssh", "id_ecdsa"),
	} {
		if signer, err := loadPrivateKey(p); err == nil {
			authMethods = append(authMethods, ssh.PublicKeys(signer))
		}
	}

	// Host key verification (use known_hosts)
	khPath := filepath.Join(homeDir, ".ssh", "known_hosts")
	hostKeyCallback, err := knownhosts.New(khPath)
	if err != nil {
		// Fallback to insecure only if known_hosts isn’t available
		hostKeyCallback = ssh.InsecureIgnoreHostKey()
	}

	cfg := &ssh.ClientConfig{
		User:            userName,
		Auth:            authMethods,
		HostKeyCallback: hostKeyCallback,
		Timeout:         10 * time.Second,
	}
	return cfg, cleanup, nil
}

func saveTokenLocally(token string) error {
	configDir, err := config.ConfigDir()
	if err != nil {
		return err
	}

	if err := os.MkdirAll(configDir, constants.ModeDirPrivate); err != nil {
		return err
	}
	_ = os.Chmod(configDir, constants.ModeDirPrivate)

	envFile := filepath.Join(configDir, constants.ConfigEnvFileName)
	env := map[string]string{}
	if existing, err := godotenv.Read(envFile); err == nil {
		maps.Copy(env, existing)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("failed to read environment file %s: %w", envFile, err)
	}

	env[constants.EnvVarAPIToken] = token

	if err := godotenv.Write(env, envFile); err != nil {
		return err
	}
	_ = os.Chmod(envFile, constants.ModeFileSecret)

	return nil
}

func loadPrivateKey(keyPath string) (ssh.Signer, error) {
	if _, err := os.Stat(keyPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("key file does not exist: %s", keyPath)
	}

	keyBytes, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read key file %s: %w", keyPath, err)
	}

	signer, err := ssh.ParsePrivateKey(keyBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse private key %s: %w", keyPath, err)
	}

	return signer, nil
}

func parseSSHTarget(target string) (user, host string) {
	currentUser := os.Getenv("USER")
	if currentUser == "" {
		currentUser = "root"
	}

	if strings.Contains(target, "@") {
		parts := strings.SplitN(target, "@", 2)
		return parts[0], parts[1]
	}

	return currentUser, target
}
