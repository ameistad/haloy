package commands

import (
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"text/template"
	"time"

	"github.com/ameistad/haloy/internal/config"
	"github.com/ameistad/haloy/internal/docker"
	"github.com/ameistad/haloy/internal/embed"
	"github.com/ameistad/haloy/internal/helpers"
	"github.com/ameistad/haloy/internal/ui"
	"github.com/spf13/cobra"
)

const (
	initTimeout = 5 * time.Minute
)

func InitCmd() *cobra.Command {
	var skipServices bool
	var withTestApp bool
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

			ctx, cancel := context.WithTimeout(context.Background(), initTimeout)
			defer cancel()

			dockerClient, err := docker.NewClient(ctx)
			if err != nil {
				ui.Error("%v", err)
				return
			}
			defer dockerClient.Close()

			var emptyDirs = []string{
				"containers/cert-storage",
				"containers/cert-storage/accounts",
				"containers/haproxy-config",
			}
			if err := copyConfigFiles(configDirPath, emptyDirs); err != nil {
				ui.Error("Failed to create configuration files: %v\n", err)
				return
			}

			// Prompt the user for email and update apps.yml.
			if err := copyConfigTemplateFiles(withTestApp); err != nil {
				ui.Error("Failed to update configuration files: %v\n", err)
				return
			}

			// Ensure default Docker network exists.
			if err := docker.EnsureNetwork(dockerClient, ctx); err != nil {
				ui.Warn("Failed to ensure Docker network exists: %v\n", err)
				ui.Warn("You can manually create it with:\n")
				ui.Warn("docker network create --driver bridge %s", config.DockerNetwork)
			}

			if !skipServices {
				if _, err := docker.EnsureServicesIsRunning(dockerClient, ctx); err != nil {
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
	cmd.Flags().BoolVar(&withTestApp, "with-test-app", false, "Add an initial test app to apps.yml")
	cmd.Flags().BoolVar(&overrideExistingConfig, "override-existing-config", false, "Override existing configuration directory if it already exists")
	return cmd
}

func copyConfigFiles(dst string, emptyDirs []string) error {
	for _, dir := range emptyDirs {
		dirPath := filepath.Join(dst, dir)
		if err := os.MkdirAll(dirPath, 0755); err != nil {
			return fmt.Errorf("failed to create empty directory %s: %w", dirPath, err)
		}
	}

	// Walk the embedded filesystem starting at the init directory.
	return fs.WalkDir(embed.InitFS, "init", func(path string, d fs.DirEntry, err error) error {
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
}

func copyConfigTemplateFiles(withTestApp bool) error {
	configDirPath, err := config.ConfigDirPath()
	if err != nil {
		return fmt.Errorf("failed to write updated config file: %w", err)
	}

	configFile := bytes.Buffer{}

	if withTestApp {
		testAppData, err := getTestAppData()
		if err != nil {
			return fmt.Errorf("failed to get test app data: %w", err)
		}
		configFileTemplateData := embed.ConfigFileWithTestAppTemplateData{
			ConfigDirPath: configDirPath,
			Domain:        testAppData.domain,
			Alias:         testAppData.alias,
			AcmeEmail:     testAppData.acmeEmail,
		}
		configFileWithTestApp, err := renderTemplate(fmt.Sprintf("templates/%s", embed.ConfigFileTemplateTest), configFileTemplateData)
		if err != nil {
			return fmt.Errorf("failed to build template for config with test app: %w", err)
		}
		configFile = configFileWithTestApp
	} else {
		data, err := embed.TemplatesFS.ReadFile(fmt.Sprintf("templates/%s", embed.ConfigFileTemplate))
		if err != nil {
			return fmt.Errorf("failed to read default apps.yml template: %w", err)
		}
		if _, err := configFile.Write(data); err != nil {
			return fmt.Errorf("failed to buffer default apps.yml: %w", err)
		}
	}

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

	// Get the full path to apps.yml.
	configFilePath, err := config.ConfigFilePath()
	if err != nil {
		return fmt.Errorf("failed to determine config file path: %w", err)
	}

	if err := os.WriteFile(configFilePath, configFile.Bytes(), 0644); err != nil {
		return fmt.Errorf("failed to write updated config file: %w", err)
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

type TestAppData struct {
	domain    string
	alias     string
	acmeEmail string
}

func getTestAppData() (TestAppData, error) {
	data := TestAppData{}

	// Prompt for email with validation
	var email string
	for {
		fmt.Print("Enter email for TLS certificates for the test-app: ")
		if _, err := fmt.Scanln(&email); err != nil {
			if err.Error() == "unexpected newline" {
				ui.Info("Email cannot be empty")
				continue
			}
			return data, fmt.Errorf("failed to read email input: %w", err)
		}

		if !helpers.IsValidEmail(email) {
			ui.Info("Please enter a valid email address")
			continue
		}
		break
	}

	data.acmeEmail = email

	ip, err := helpers.GetExternalIP()
	if err != nil {
		return data, fmt.Errorf("failed to get external IP: %w", err)
	}

	data.domain = fmt.Sprintf("%s.nip.io", ip.String())
	data.alias = fmt.Sprintf("www.%s.nip.io", ip.String())

	return data, nil
}
