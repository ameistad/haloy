package haloyadm

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/template"

	"filippo.io/age"
	"github.com/ameistad/haloy/internal/config"
	"github.com/ameistad/haloy/internal/constants"
	"github.com/ameistad/haloy/internal/embed"
	"github.com/ameistad/haloy/internal/ui"
	"github.com/joho/godotenv"
	"github.com/spf13/cobra"
)

const (
	apiTokenLength = 32 // bytes, results in 64 character hex string
)

func InitCmd() *cobra.Command {
	var skipServices bool
	var override bool
	var apiDomain string
	var acmeEmail string
	var devMode bool
	var debug bool
	var noLogs bool
	var localInstall bool

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize Haloy data directory and start core services",
		Long: fmt.Sprintf(
			`Initialize Haloy by creating the data directory structure and starting core services.

Installation modes:
  Default (system): Uses system directories (/etc/haloy, /var/lib/haloy) when running as root
  --local-install:  Forces user directories (~/.config/haloy, ~/.local/share/haloy)

This command will:
- Create the data directory (default: /var/lib/haloy)
- Create the config directory for haloyd (default: /etc/haloy)
- Create the Docker network for Haloy services
- Start HAProxy and haloyd containers (unless --no-services is used)

The data directory can be customized by setting the %s environment variable.`,
			constants.EnvVarDataDir,
		),
		Run: func(cmd *cobra.Command, args []string) {
			ctx := cmd.Context()

			if localInstall {
				os.Setenv(constants.EnvVarSystemInstall, "false")
			}

			if !config.IsSystemMode() {
				ui.Info("Installing in local mode (user directories)")
			}

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

			dataDir, err := config.DataDir()
			if err != nil {
				ui.Error("Failed to determine data directory: %v\n", err)
				return
			}

			configDir, err := config.ConfigDir()
			if err != nil {
				ui.Error("Failed to determine haloyd config directory: %v\n", err)
				return
			}

			if err := validateAndPrepareDirectory(configDir, "Haloyd Config", override); err != nil {
				ui.Error("%v\n", err)
				return
			}
			createdDirs = append(createdDirs, configDir)

			if err := validateAndPrepareDirectory(dataDir, "Data", override); err != nil {
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
			if err := createConfigFiles(apiToken, identity.String(), apiDomain, acmeEmail, configDir); err != nil {
				ui.Error("Failed to create config files: %v\n", err)
				return
			}

			emptyDirs := []string{
				filepath.Base(constants.HAProxyConfigDir),
				filepath.Base(constants.DBDir),
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

			successMsg := "Haloy initialized successfully!\n\n"
			successMsg += fmt.Sprintf("📁 Data directory: %s\n", dataDir)
			successMsg += fmt.Sprintf("⚙️ Config directory: %s\n", configDir)
			if apiDomain != "" {
				successMsg += fmt.Sprintf("🌐 haloyd domain: %s\n", apiDomain)
			}
			ui.Success("%s", successMsg)

			cleanupOnFailure = false

			// Start the haloyd container and haproxy container, stream logs if requested.
			if !skipServices {
				ui.Info("Starting Haloy services...")
				if err := startServices(ctx, dataDir, configDir, devMode, override, debug); err != nil {
					ui.Error("%s", err)
					return
				}

				if !noLogs {
					ui.Info("Waiting for haloyd API to become available...")
					// Wait for API to become available and stream init logs
					if err := streamHaloydInitLogs(ctx, apiToken); err != nil {
						ui.Warn("Failed to stream haloyd initialization logs: %v", err)
						ui.Info("haloyd is starting in the background. Check logs with: docker logs haloyd")
					}
				}
			}

			apiDomainMessage := "<server-url>"
			if apiDomain != "" {
				apiDomainMessage = apiDomain
			}
			ui.Info("You can now add this server to the haloy cli with:")
			ui.Info(" haloy server add %s %s", apiDomainMessage, apiToken)
		},
	}

	cmd.Flags().BoolVar(&skipServices, "no-services", false, "Skip starting HAProxy and haloyd containers")
	cmd.Flags().BoolVar(&override, "override", false, "Remove and recreate existing data directory. Any existing haloyd or haproxy containers will be restarted.")
	cmd.Flags().StringVar(&apiDomain, "api-domain", "", "Domain for the haloyd API (e.g., api.yourserver.com)")
	cmd.Flags().StringVar(&acmeEmail, "acme-email", "", "Email address for Let's Encrypt certificate registration")
	cmd.Flags().BoolVar(&devMode, "dev", false, "Start in development mode using the local haloyd image")
	cmd.Flags().BoolVar(&debug, "debug", false, "Enable debug mode")
	cmd.Flags().BoolVar(&noLogs, "no-logs", false, "Don't stream haloyd initialization logs")
	cmd.Flags().BoolVar(&localInstall, "local-install", false, "Install in user directories instead of system directories")

	return cmd
}

func copyDataFiles(dataDir string, emptyDirs []string) error {
	// First create empty directories
	for _, dir := range emptyDirs {
		dirPath := filepath.Join(dataDir, dir)
		if err := os.MkdirAll(dirPath, constants.ModeDirPrivate); err != nil {
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

		targetPath := filepath.Join(dataDir, relPath)
		if d.IsDir() {
			if err := os.MkdirAll(targetPath, constants.ModeDirPrivate); err != nil {
				return err
			}
			if err := os.Chmod(targetPath, constants.ModeDirPrivate); err != nil {
				ui.Warn("failed to chmod %s: %v", targetPath, err)
			}
			return nil // continue walking; children will be visited next
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
		fileMode := constants.ModeFileDefault
		if filepath.Ext(targetPath) == ".sh" {
			fileMode = constants.ModeFileExec
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
	if err := copyConfigTemplateFiles(dataDir); err != nil {
		return fmt.Errorf("failed to copy template files: %w", err)
	}

	return nil
}

func copyConfigTemplateFiles(dataDir string) error {
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

	haproxyConfigFilePath := filepath.Join(dataDir, constants.HAProxyConfigDir, constants.HAProxyConfigFileName)

	if err := os.WriteFile(haproxyConfigFilePath, haproxyConfigFile.Bytes(), constants.ModeFileDefault); err != nil {
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
	envPath := filepath.Join(configDir, constants.ConfigEnvFileName)
	env := map[string]string{
		constants.EnvVarAPIToken:    apiToken,
		constants.EnvVarAgeIdentity: encryptionKey,
	}
	if err := godotenv.Write(env, envPath); err != nil {
		return fmt.Errorf("failed to write %s content: %w", constants.ConfigEnvFileName, err)
	}

	if err := os.Chmod(envPath, constants.ModeFileSecret); err != nil {
		return fmt.Errorf("failed to set %s file permissions: %w", constants.ConfigEnvFileName, err)
	}

	if domain != "" {
		haloydConfig := &config.HaloydConfig{}
		haloydConfig.API.Domain = domain
		haloydConfig.Certificates.AcmeEmail = acmeEmail

		if err := haloydConfig.Validate(); err != nil {
			return fmt.Errorf("invalid haloyd config: %w", err)
		}

		haloydConfigPath := filepath.Join(configDir, constants.HaloydConfigFileName)
		if err := config.SaveHaloydConfig(haloydConfig, haloydConfigPath); err != nil {
			return fmt.Errorf("failed to save haloyd config: %w", err)
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
			return fmt.Errorf("%s directory already exists: %s\nUse --override to overwrite",
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

	if err := os.MkdirAll(dirPath, constants.ModeDirPrivate); err != nil {
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
