package deploy

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ameistad/haloy/internal/config"
	"github.com/ameistad/haloy/internal/ui"
	"gopkg.in/yaml.v3"
)

// WriteAppConfigHistory writes the given appConfig to the history folder, naming the file <deploymentID>.yml.
func writeAppConfigHistory(appConfig *config.AppConfig, deploymentID string, deploymentsToKeep int) error {
	// Define the history directory inside the config directory.
	historyPath, err := config.HistoryPath()
	if err != nil {
		return fmt.Errorf("failed to get history directory: %w", err)
	}

	// Create the file name based on the deploymentID.
	historyFilePath := filepath.Join(historyPath, fmt.Sprintf("%s.yml", deploymentID))

	// Marshal the appConfig struct to YAML.
	data, err := yaml.Marshal(appConfig)
	if err != nil {
		return fmt.Errorf("failed to marshal app config: %w", err)
	}

	// Write the YAML data to the file.
	if err := os.WriteFile(historyFilePath, data, 0644); err != nil {
		return fmt.Errorf("failed to write config history file '%s': %w", historyFilePath, err)
	}

	// After writing, prune old history files.
	// List all history files ending with .yml in the history directory.
	files, err := os.ReadDir(historyPath)
	if err != nil {
		return fmt.Errorf("failed to read history directory '%s': %w", historyPath, err)
	}

	var historyFiles []os.DirEntry
	for _, file := range files {
		// Only consider files that are not directories and have a .yml extension, and are not the current deployment file.
		if !file.IsDir() && strings.HasSuffix(file.Name(), ".yml") && file.Name() != fmt.Sprintf("%s.yml", deploymentID) {
			historyFiles = append(historyFiles, file)
		}
	}

	// Sort the files descending by filename (deployment id).
	sort.Slice(historyFiles, func(i, j int) bool {
		return historyFiles[i].Name() > historyFiles[j].Name()
	})

	// Delete files beyond the deploymentsToKeep count.
	if len(historyFiles) > deploymentsToKeep {
		for i := deploymentsToKeep; i < len(historyFiles); i++ {
			filePath := filepath.Join(historyPath, historyFiles[i].Name())
			if err := os.Remove(filePath); err != nil {
				ui.Warn("Failed to remove old history file %s: %v", filePath, err)
			} else {
				ui.Info("Removed old history file %s", filePath)
			}
		}
	}

	return nil
}

// GetAppConfigHistory loads the AppConfig from the history file with the given deploymentID.
// It reads the file from config.HistoryPath and unmarshals the YAML data into an AppConfig struct.
func GetAppConfigHistory(deploymentID string) (*config.AppConfig, error) {
	historyPath, err := config.HistoryPath()
	if err != nil {
		return nil, fmt.Errorf("failed to get history path: %w", err)
	}
	filePath := filepath.Join(historyPath, fmt.Sprintf("%s.yml", deploymentID))

	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read history file '%s': %w", filePath, err)
	}

	var appConfig config.AppConfig
	if err := yaml.Unmarshal(data, &appConfig); err != nil {
		return nil, fmt.Errorf("failed to unmarshal history file '%s': %w", filePath, err)
	}
	return &appConfig, nil
}
