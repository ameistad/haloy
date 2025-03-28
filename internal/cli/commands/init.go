package commands

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"text/template"

	"github.com/ameistad/haloy/internal/config"
	"github.com/ameistad/haloy/internal/embed"
	"github.com/ameistad/haloy/internal/ui"
	"github.com/spf13/cobra"
)

func InitCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize configuration files and prepare HAProxy for production",
		RunE: func(cmd *cobra.Command, args []string) error {
			configDir, err := config.ConfigDirPath()
			if err != nil {
				return fmt.Errorf("failed to determine config directory: %w", err)
			}

			if _, err := os.Stat(configDir); err == nil {
				fmt.Println("Warning: Configuration directory already exists. Files may be overwritten.")
			}

			var emptyDirs = []string{
				"containers/cert-storage",
				"containers/haproxy-config",
			}
			if err := copyConfigFiles(configDir, emptyDirs); err != nil {
				return err
			}

			// Prompt the user for email and update apps.yml.
			if err := copyConfigTemplateFiles(); err != nil {
				return err
			}

			ui.Success("Configuration files created successfully in %s\n", configDir)
			ui.Info("Add your applications to apps.yml and run:.\n")
			ui.Command("haloy deploy <app-name>")
			return nil
		},
	}

	return cmd
}

func copyConfigFiles(dst string, emptyDirs []string) error {
	if err := os.MkdirAll(dst, 0755); err != nil {
		return fmt.Errorf("failed to create destination directory: %w", err)
	}

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

func copyConfigTemplateFiles() error {
	// Prompt for email with validation
	// var email string
	// for {
	// 	fmt.Print("Enter email for Let's Encrypt TLS certificates: ")
	// 	if _, err := fmt.Scanln(&email); err != nil {
	// 		if err.Error() == "unexpected newline" {
	// 			fmt.Println("Email cannot be empty")
	// 			continue
	// 		}
	// 		return fmt.Errorf("failed to read email input: %w", err)
	// 	}

	// 	if !helpers.IsValidEmail(email) {
	// 		fmt.Println("Please enter a valid email address")
	// 		continue
	// 	}
	// 	break
	// }

	configDirPath, err := config.ConfigDirPath()
	if err != nil {
		return fmt.Errorf("failed to write updated config file: %w", err)
	}
	configFileTemplateData := struct {
		ConfigDirPath string
	}{
		ConfigDirPath: configDirPath,
	}
	configFile, err := renderTemplate(fmt.Sprintf("templates/%s", config.ConfigFileName), configFileTemplateData)
	if err != nil {
		return fmt.Errorf("failed to build template: %w", err)
	}

	haproxyConfigTemplateData := struct {
		HTTPFrontend  string
		HTTPSFrontend string
		Backends      string
	}{
		HTTPFrontend:  "",
		HTTPSFrontend: "",
		Backends:      "",
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
