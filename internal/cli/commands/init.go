package commands

import (
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/template"
	"time"

	"github.com/ameistad/haloy/internal/config"
	"github.com/ameistad/haloy/internal/docker"
	"github.com/ameistad/haloy/internal/embed"
	"github.com/ameistad/haloy/internal/ui"
	"github.com/spf13/cobra"
)

const (
	initTimeout = 5 * time.Minute
)

func InitCmd() *cobra.Command {
	var skipServices bool
	var overrideExistingConfig bool

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize configuration files and prepare HAProxy for production",
		Run: func(cmd *cobra.Command, args []string) {
			configDirPath, err := config.ConfigDirPath()
			if err != nil {
				ui.Error("Failed to determine config directory: %v\n", err)
				return
			}

			// Check the status of the configuration directory
			fileInfo, statErr := os.Stat(configDirPath)
			if statErr == nil { // Directory exists
				if !fileInfo.IsDir() {
					ui.Error("Configuration path '%s' exists but is not a directory. Please remove it or use a different path.", configDirPath)
					return
				}
				// Directory exists
				if !overrideExistingConfig {
					ui.Error("Configuration directory '%s' already exists. Use --override-existing-config to overwrite.", configDirPath)
					return
				}
				// overrideExistingConfig is true, so remove the existing directory
				ui.Info("Removing existing configuration directory: %s\n", configDirPath)
				if err := os.RemoveAll(configDirPath); err != nil {
					ui.Error("Failed to remove existing config directory: %v\n", err)
					return
				}
				// Proceed to create the directory below
			} else if !os.IsNotExist(statErr) {
				// An error other than "does not exist" occurred with os.Stat
				ui.Error("Failed to check status of config directory '%s': %v\n", configDirPath, statErr)
				return
			}
			// At this point, the directory either did not exist,
			// or it existed, was a directory, and has been removed (if overrideExistingConfig was true).
			// Now, (re)create the configuration directory.
			ui.Info("Creating configuration directory: %s\n", configDirPath)
			if err := os.MkdirAll(configDirPath, 0755); err != nil {
				ui.Error("Failed to create config directory '%s': %v\n", configDirPath, err)
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

			ctx, cancel := context.WithTimeout(context.Background(), initTimeout)
			defer cancel()

			cli, err := docker.NewClient(ctx)
			if err != nil {
				if os.IsPermission(err) ||
					strings.Contains(err.Error(), "permission denied") ||
					strings.Contains(err.Error(), "connect: permission denied") ||
					strings.Contains(err.Error(), "Got permission denied while trying to connect to the Docker daemon socket") {
					ui.Error("%v", err)
					ui.Warn("It looks like your user does not have permission to access the Docker daemon socket.")
					ui.Warn("You may need to add your user to the 'docker' group and restart your session:")
					ui.Warn("  sudo usermod -aG docker $(whoami)")
					ui.Warn("  # Then log out and log back in, or run: newgrp docker")
					return
				}
				ui.Error("%v", err)
				return
			}
			defer cli.Close()

			var emptyDirs = []string{
				"containers/cert-storage",
				"containers/cert-storage/accounts",
				"containers/haproxy-config",
			}
			if err := copyConfigFiles(configDirPath, emptyDirs); err != nil {
				ui.Error("Failed to create configuration files: %v\n", err)
				return
			}

			// Ensure default Docker network exists.
			if err := docker.EnsureNetwork(cli, ctx); err != nil {
				ui.Warn("Failed to ensure Docker network exists: %v\n", err)
				ui.Warn("You can manually create it with:\n")
				ui.Warn("docker network create --driver bridge %s", config.DockerNetwork)
			}

			if !skipServices {
				if _, err := docker.EnsureServicesIsRunning(cli, ctx); err != nil {
					ui.Error("Failed to to start haproxy and haloy-manager: %v\n", err)
					return
				}

			}

			successMsg := fmt.Sprintf("Configuration files created successfully in %s\n", configDirPath)
			if !skipServices {
				successMsg += "HAProxy and haloy-manager started successfully.\n"
			}
			successMsg += "You can now add your applications to apps.yml and run:\n"
			successMsg += "haloy deploy <app-name>"
			ui.Success("%s", successMsg)
		},
	}

	cmd.Flags().BoolVar(&skipServices, "no-services", false, "Initialize configuration files without starting Docker services")
	cmd.Flags().BoolVar(&overrideExistingConfig, "override-existing-config", false, "Override existing configuration directory if it already exists")
	return cmd
}

func copyConfigFiles(dst string, emptyDirs []string) error {
	// First create empty directories
	for _, dir := range emptyDirs {
		dirPath := filepath.Join(dst, dir)
		if err := os.MkdirAll(dirPath, 0755); err != nil {
			return fmt.Errorf("failed to create empty directory %s: %w", dirPath, err)
		}
	}

	// Copy static files from embedded filesystem
	err := fs.WalkDir(embed.InitFS, "init", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return fmt.Errorf("error walking embedded filesystem: %w", err)
		}

		// Compute the relative path based on the init directory.
		relPath, err := filepath.Rel("init", path)
		if err != nil {
			return fmt.Errorf("failed to determine relative path: %w", err)
		}

		targetPath := filepath.Join(dst, relPath)
		if d.IsDir() {
			return os.MkdirAll(targetPath, 0755)
		}

		// Skip template files - they'll be handled by copyConfigTemplateFiles
		if strings.Contains(path, "template") || filepath.Ext(path) == ".tmpl" {
			return nil
		}

		// Read the file from the embed FS.
		data, err := embed.InitFS.ReadFile(path)
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
