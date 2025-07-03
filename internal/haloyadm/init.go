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

	"github.com/ameistad/haloy/internal/config"
	"github.com/ameistad/haloy/internal/embed"
	"github.com/ameistad/haloy/internal/ui"
	"github.com/spf13/cobra"
)

const (
	initTimeout = 5 * time.Minute
)

func InitCmd() *cobra.Command {
	var skipServices bool
	var overrideExisting bool

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize Haloy data directory and start core services",
		Long: `Initialize Haloy by creating the data directory structure and starting core services.

This command will:
- Create the data directory (default: ~/.local/share/haloy)
- Copy initial files and templates
- Create the Docker network for Haloy services
- Start HAProxy and haloy-manager containers (unless --no-services is used)

The data directory can be customized by setting the HALOY_DATA_DIR environment variable.`,
		Run: func(cmd *cobra.Command, args []string) {
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

			// Check the status of the data directory
			fileInfo, statErr := os.Stat(dataDir)
			if statErr == nil { // Directory exists
				if !fileInfo.IsDir() {
					ui.Error("Data directory path '%s' exists but is not a directory. Please remove it or use a different path.", dataDir)
					return
				}
				// Directory exists
				if !overrideExisting {
					ui.Error("Data directory '%s' already exists. Use --override-existing to overwrite.", dataDir)
					return
				}
				// overrideExisting is true, so remove the existing directory
				ui.Info("Removing existing data directory at: %s\n", dataDir)
				if err := os.RemoveAll(dataDir); err != nil {
					ui.Error("Failed to remove existing data directory: %v\n", err)
					return
				}
				// Proceed to create the directory below
			} else if !os.IsNotExist(statErr) {
				// An error other than "does not exist" occurred with os.Stat
				ui.Error("Failed to check status of data directory '%s': %v\n", dataDir, statErr)
				return
			}
			// At this point, the directory either did not exist,
			// or it existed, was a directory, and has been removed (if overrideExisting was true).
			// Now, (re)create the configuration directory.
			if err := os.MkdirAll(dataDir, 0755); err != nil {
				ui.Error("Failed to create data directory '%s': %v\n", dataDir, err)
				return
			}

			// Check if Docker is installed and available in PATH.
			_, err = exec.LookPath("docker")
			if err != nil {
				ui.Error("Docker executable not found.\n" +
					"Please ensure Docker is installed and that its binary is in your system's PATH.\n" +
					"You can download and install Docker from: https://www.docker.com/get-started\n" +
					"If Docker is installed, verify your PATH environment variable includes the directory where Docker is located.")
				return
			}

			apiToken, err := generateAPIToken()
			if err != nil {
				ui.Error("Failed to generate API token: %v\n", err)
				return
			}

			if err := createEnvFile(apiToken, configDir); err != nil {
				ui.Error("Failed to create .env file: %v\n", err)
				return
			}

			var emptyDirs = []string{
				"haproxy-config",
			}
			if err := copyDataFiles(dataDir, emptyDirs); err != nil {
				ui.Error("Failed to create configuration files: %v\n", err)
				return
			}

			// Ensure default Docker network exists.
			if err := ensureNetwork(ctx); err != nil {
				ui.Warn("Failed to ensure Docker network exists: %v\n", err)
				ui.Warn("You can manually create it with:\n")
				ui.Warn("docker network create --driver bridge %s", config.DockerNetwork)
			}

			// Start the haloy-manager container and haproxy container.
			if !skipServices {
				if err := startServices(ctx, false); err != nil {
					ui.Error("%s", err)
					return
				}
			}

			successMsg := "Haloy initialized successfully!\n"
			successMsg += fmt.Sprintf("Data directory: %s\n", dataDir)
			if !skipServices {
				successMsg += "HAProxy and haloy-manager started successfully.\n"
			}
			ui.Success("%s", successMsg)
		},
	}

	cmd.Flags().BoolVar(&skipServices, "no-services", false, "Skip starting HAProxy and haloy-manager containers")
	cmd.Flags().BoolVar(&overrideExisting, "override-existing", false, "Remove and recreate existing data directory")
	return cmd
}

func copyDataFiles(dst string, emptyDirs []string) error {
	// First create empty directories
	for _, dir := range emptyDirs {
		dirPath := filepath.Join(dst, dir)
		if err := os.MkdirAll(dirPath, 0755); err != nil {
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
			return os.MkdirAll(targetPath, 0755)
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
		fileMode := fs.FileMode(0644)
		if filepath.Ext(targetPath) == ".sh" {
			fileMode = 0755
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

	haproxyConfigFile, err := renderTemplate(fmt.Sprintf("templates/%s", config.HAProxyConfigFileName), haproxyConfigTemplateData)
	if err != nil {
		return fmt.Errorf("failed to build HAProxy template: %w", err)
	}

	haproxyConfigFilePath, err := config.HAProxyConfigFilePath()
	if err != nil {
		return fmt.Errorf("failed to determine HAProxy config file path: %w", err)
	}

	if err := os.WriteFile(haproxyConfigFilePath, haproxyConfigFile.Bytes(), 0644); err != nil {
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
	bytes := make([]byte, 32) // 64 character hex string
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("failed to generate random token: %w", err)
	}
	return hex.EncodeToString(bytes), nil
}

// createEnvFile creates a .env file with the API token in the data directory
func createEnvFile(apiToken, dataDir string) error {
	envContent := fmt.Sprintf("# Haloy API Token - Keep this secure!\nHALOY_API_TOKEN=%s\n", apiToken)
	envPath := filepath.Join(dataDir, ".env")

	// Create .env file with strict permissions (owner read/write only)
	if err := os.WriteFile(envPath, []byte(envContent), 0600); err != nil {
		return fmt.Errorf("failed to create .env file: %w", err)
	}
	return nil
}

// EnsureNetworkCmd checks for the existence of the specified Docker network and creates it if it doesn't exist.
func ensureNetwork(ctx context.Context) error {
	// List networks filtering by name
	// The --format option outputs only the network names.
	cmdList := exec.CommandContext(ctx, "docker", "network", "ls", "--filter", fmt.Sprintf("name=%s", config.DockerNetwork), "--format", "{{.Name}}")
	var out bytes.Buffer
	cmdList.Stdout = &out
	if err := cmdList.Run(); err != nil {
		return fmt.Errorf("failed to list Docker networks: %w", err)
	}

	// Check if the network exists.
	networks := strings.Split(strings.TrimSpace(out.String()), "\n")
	networkExists := false
	for _, n := range networks {
		if n == config.DockerNetwork {
			networkExists = true
			break
		}
	}

	if networkExists {
		return nil // Already exists.
	}

	// Create the network if it doesn't exist.
	// Here we create a bridge network that is attachable and assign a label.
	cmdCreate := exec.CommandContext(ctx, "docker", "network", "create",
		"--driver", "bridge",
		"--attachable",
		"--label", "created-by=haloy",
		config.DockerNetwork,
	)
	if output, err := cmdCreate.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to create Docker network: %w - output: %s", err, output)
	}

	return nil
}
