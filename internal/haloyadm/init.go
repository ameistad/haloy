package haloyadm

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/template"
	"time"

	"filippo.io/age"
	"github.com/ameistad/haloy/internal/config"
	"github.com/ameistad/haloy/internal/constants"
	"github.com/ameistad/haloy/internal/embed"
	"github.com/ameistad/haloy/internal/ui"
	"github.com/spf13/cobra"
)

const (
	initTimeout    = 5 * time.Minute
	apiTokenLength = 32                // bytes, results in 64 character hex string
	envFileMode    = os.FileMode(0600) // owner read/write only
	configFileMode = os.FileMode(0644) // owner read/write, group/others read
	executableMode = os.FileMode(0755) // owner read/write/execute, group/others read/execute
)

func InitCmd() *cobra.Command {
	var skipServices bool
	var overrideExisting bool
	var managerDomain string
	var acmeEmail string
	var debug bool

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize Haloy data directory and start core services",
		Long: `Initialize Haloy by creating the data directory structure and starting core services.

This command will:
- Create the config directory (default: ~/.config/haloy)
- Create the data directory (default: ~/.local/share/haloy)
- Copy initial files and templates
- Create the Docker network for Haloy services
- Start HAProxy and haloy-manager containers (unless --no-services is used)

The data directory can be customized by setting the HALOY_DATA_DIR environment variable.`,
		Run: func(cmd *cobra.Command, args []string) {

			var createdDirs []string
			var cleanupOnFailure bool = true

			// If we encounter an error, we will clean up any directories created so far.
			defer func() {
				if cleanupOnFailure && len(createdDirs) > 0 {
					cleanupDirectories(createdDirs)
				}
			}()

			// Check if Docker is installed and available in PATH.
			if _, err := exec.LookPath("docker"); err != nil {
				ui.Error("Docker executable not found.\n" +
					"Please ensure Docker is installed and in your PATH.\n" +
					"Download from: https://www.docker.com/get-started")
				return
			}

			ctx, cancel := context.WithTimeout(context.Background(), initTimeout)
			defer cancel()

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

			if err := validateAndPrepareDirectory(configDir, "Config", overrideExisting); err != nil {
				ui.Error("%v\n", err)
				return
			}
			createdDirs = append(createdDirs, configDir)

			if err := validateAndPrepareDirectory(dataDir, "Data", overrideExisting); err != nil {
				ui.Error("%v\n", err)
				return
			}
			createdDirs = append(createdDirs, dataDir)

			apiToken, err := generateAPIToken()
			if err != nil {
				ui.Error("Failed to generate API token: %v\n", err)
				return
			}

			// Generate age identity
			identity, err := age.GenerateX25519Identity()
			if err != nil {
				ui.Error("Failed to generate age identity: %v\n", err)
				return
			}

			// Use createdDirs for cleanup if later steps fail
			if err := createConfigFiles(apiToken, identity.String(), managerDomain, acmeEmail, configDir); err != nil {
				ui.Error("Failed to create config files: %v\n", err)
				return
			}

			var emptyDirs = []string{
				filepath.Base(constants.HAProxyConfigPath),
				filepath.Base(constants.DBPath),
			}
			if err := copyDataFiles(dataDir, emptyDirs); err != nil {
				ui.Error("Failed to create configuration files: %v\n", err)
				return
			}

			// Ensure default Docker network exists.
			if err := ensureNetwork(ctx); err != nil {
				ui.Warn("Failed to ensure Docker network exists: %v\n", err)
				ui.Warn("You can manually create it with:\n")
				ui.Warn("docker network create --driver bridge %s", constants.DockerNetwork)
			}

			// Start the haloy-manager container and haproxy container.
			if !skipServices {
				if err := startServices(ctx, dataDir, configDir, false, overrideExisting, debug); err != nil {
					ui.Error("%s", err)
					return
				}
			}

			successMsg := "Haloy initialized successfully!\n\n"
			successMsg += fmt.Sprintf("📁 Data directory: %s\n", dataDir)
			successMsg += fmt.Sprintf("⚙️ Config directory: %s\n", configDir)
			if managerDomain != "" {
				successMsg += fmt.Sprintf("🌐 Manager domain: %s\n", managerDomain)
			}
			if !skipServices {
				successMsg += "\n✅ HAProxy and haloy-manager started successfully.\n"
				successMsg += "   Use 'haloy status' to check service health.\n"
			}
			cleanupOnFailure = false
			ui.Success("%s", successMsg)
		},
	}

	cmd.Flags().BoolVar(&skipServices, "no-services", false, "Skip starting HAProxy and haloy-manager containers")
	cmd.Flags().BoolVar(&overrideExisting, "override-existing", false, "Remove and recreate existing data directory. Any existing haloy-manager or haproxy containers will be restarted.")
	cmd.Flags().StringVar(&managerDomain, "domain", "", "Domain for the Haloy manager API (e.g., api.yourserver.com)")
	cmd.Flags().StringVar(&acmeEmail, "acme-email", "", "Email address for Let's Encrypt certificate registration")
	cmd.Flags().BoolVar(&debug, "debug", false, "Enable debug mode")
	return cmd
}

func copyDataFiles(dst string, emptyDirs []string) error {
	// First create empty directories
	for _, dir := range emptyDirs {
		dirPath := filepath.Join(dst, dir)
		if err := os.MkdirAll(dirPath, executableMode); err != nil {
			return fmt.Errorf("failed to create empty directory %s: %w", dirPath, err)
		}
	}

	// Copy static files from embedded filesystem to data directory.
	err := fs.WalkDir(embed.DataFS, "data", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return fmt.Errorf("error walking embedded filesystem: %w", err)
		}

		// Compute the relative path based on the data directory.
		relPath, err := filepath.Rel("data", path)
		if err != nil {
			return fmt.Errorf("failed to determine relative path: %w", err)
		}

		targetPath := filepath.Join(dst, relPath)
		if d.IsDir() {
			return os.MkdirAll(targetPath, executableMode)
		}

		// Skip template files - they'll be handled by copyConfigTemplateFiles
		if strings.Contains(path, "template") {
			return nil
		}

		data, err := embed.DataFS.ReadFile(path)
		if err != nil {
			return fmt.Errorf("failed to read embedded file %s: %w", path, err)
		}

		// Determine the file mode - make shell scripts executable
		fileMode := configFileMode
		if filepath.Ext(targetPath) == ".sh" {
			fileMode = executableMode
		}

		if err := os.WriteFile(targetPath, data, fileMode); err != nil {
			return fmt.Errorf("failed to write file %s: %w", targetPath, err)
		}

		return nil
	})

	if err != nil {
		return err
	}

	// Now handle template files
	if err := copyConfigTemplateFiles(); err != nil {
		return fmt.Errorf("failed to copy template files: %w", err)
	}

	return nil
}
func copyConfigTemplateFiles() error {
	haproxyConfigTemplateData := embed.HAProxyTemplateData{
		HTTPFrontend:            "",
		HTTPSFrontend:           "",
		HTTPSFrontendUseBackend: "",
		Backends:                "",
	}

	haproxyConfigFile, err := renderTemplate(fmt.Sprintf("templates/%s", constants.HAProxyConfigFileName), haproxyConfigTemplateData)
	if err != nil {
		return fmt.Errorf("failed to build HAProxy template: %w", err)
	}

	haproxyConfigFilePath, err := config.HAProxyConfigFilePath()
	if err != nil {
		return fmt.Errorf("failed to determine HAProxy config file path: %w", err)
	}

	if err := os.WriteFile(haproxyConfigFilePath, haproxyConfigFile.Bytes(), configFileMode); err != nil {
		return fmt.Errorf("failed to write updated haproxy config file: %w", err)
	}

	return nil
}

func renderTemplate(templateFilePath string, templateData any) (bytes.Buffer, error) {
	var buf bytes.Buffer
	file, err := embed.TemplatesFS.ReadFile(templateFilePath)
	if err != nil {
		return buf, fmt.Errorf("failed to read embedded file: %w", err)
	}

	tmpl, err := template.New(templateFilePath).Parse(string(file))
	if err != nil {
		return buf, fmt.Errorf("failed to parse template: %w", err)
	}

	if err := tmpl.Execute(&buf, templateData); err != nil {
		return buf, fmt.Errorf("failed to execute template: %w", err)
	}
	return buf, nil
}

// generateAPIToken creates a secure random API token
func generateAPIToken() (string, error) {
	tokenBytes := make([]byte, apiTokenLength)
	if _, err := rand.Read(tokenBytes); err != nil {
		return "", fmt.Errorf("failed to generate random token: %w", err)
	}
	token := hex.EncodeToString(tokenBytes)

	// Validate generated token
	if len(token) != apiTokenLength*2 {
		return "", fmt.Errorf("generated token has unexpected length: got %d, expected %d",
			len(token), apiTokenLength*2)
	}

	return token, nil
}

// createConfigFiles creates a .env file with the API token in the data directory
func createConfigFiles(apiToken, encryptionKey, domain, acmeEmail, configDir string) error {
	if apiToken == "" {
		return fmt.Errorf("apiToken cannot be empty")
	}
	if encryptionKey == "" {
		return fmt.Errorf("encryptionKey cannot be empty")
	}
	if configDir == "" {
		return fmt.Errorf("configDir cannot be empty")
	}
	envPath := filepath.Join(configDir, ".env")
	envFile, err := os.OpenFile(envPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, envFileMode)
	if err != nil {
		return fmt.Errorf("failed to create .env file: %w", err)
	}
	defer envFile.Close()

	envContent := fmt.Sprintf(`# Haloy Configuration
# API token for haloy-manager authentication
HALOY_API_TOKEN=%s

# Encryption key for secrets (age X25519 private key)
HALOY_ENCRYPTION_KEY=%s
`, apiToken, encryptionKey)

	if _, err := envFile.WriteString(envContent); err != nil {
		return fmt.Errorf("failed to write .env content: %w", err)
	}

	if domain != "" {
		managerConfig := &config.ManagerConfig{}
		managerConfig.API.Domain = domain
		managerConfig.Certificates.AcmeEmail = acmeEmail

		if err := managerConfig.Validate(); err != nil {
			return fmt.Errorf("invalid manager config: %w", err)
		}

		managerConfigPath := filepath.Join(configDir, constants.ManagerConfigFileName)
		if err := config.Save(managerConfig, managerConfigPath); err != nil {
			return fmt.Errorf("failed to save manager config: %w", err)
		}
	}
	return nil
}

func validateAndPrepareDirectory(dirPath, dirType string, overrideExisting bool) error {
	fileInfo, statErr := os.Stat(dirPath)
	if statErr == nil {
		if !fileInfo.IsDir() {
			return fmt.Errorf("%s directory path exists but is a file, not a directory: %s",
				strings.ToLower(dirType), dirPath)
		}
		if !overrideExisting {
			return fmt.Errorf("%s directory already exists: %s\nUse --override-existing to overwrite",
				strings.ToLower(dirType), dirPath)
		}
		ui.Info("Removing existing %s directory: %s\n", strings.ToLower(dirType), dirPath)
		if err := os.RemoveAll(dirPath); err != nil {
			return fmt.Errorf("failed to remove existing %s directory %s: %w",
				strings.ToLower(dirType), dirPath, err)
		}
	} else if !os.IsNotExist(statErr) {
		return fmt.Errorf("failed to access %s directory %s: %w",
			strings.ToLower(dirType), dirPath, statErr)
	}

	if err := os.MkdirAll(dirPath, executableMode); err != nil {
		return fmt.Errorf("failed to create %s directory %s: %w",
			strings.ToLower(dirType), dirPath, err)
	}
	return nil
}

func cleanupDirectories(dirs []string) {
	for _, dir := range dirs {
		if err := os.RemoveAll(dir); err != nil {
			ui.Warn("Failed to cleanup directory %s: %v", dir, err)
		}
	}
}
