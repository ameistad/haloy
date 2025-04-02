package config

import (
	"fmt"
	"os"
	"path/filepath"
)

func CheckConfigDirExists(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("directory %s does not exist", path)
		}
		return fmt.Errorf("failed to access directory: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("path %s is not a directory", path)
	}
	return nil
}

// Defaults to ~/.config/haloy
// If HALOY_CONFIG_PATH is set, it will use that instead.
func ConfigDirPath() (string, error) {

	// First check if HALOY_CONFIG_PATH is set.
	if envPath, ok := os.LookupEnv("HALOY_CONFIG_PATH"); ok && envPath != "" {
		if err := CheckConfigDirExists(envPath); err != nil {
			return "", fmt.Errorf("HALOY_CONFIG_PATH is set to '%s' but it is not a valid directory: %w", envPath, err)
		}
		return envPath, nil

	}

	// Fallback to the default path.
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	defaultPath := filepath.Join(home, ".config", "haloy")
	if err := CheckConfigDirExists(defaultPath); err != nil {
		return "", fmt.Errorf("default config directory '%s' does not exist: %w", defaultPath, err)
	}
	return defaultPath, nil
}

// ConfigFilePath returns "~/.config/haloy/apps.yml".
func ConfigFilePath() (string, error) {
	configDirPath, err := ConfigDirPath()
	if err != nil {
		return "", err
	}
	return filepath.Join(configDirPath, ConfigFileName), nil
}

func ConfigContainersPath() (string, error) {
	configDirPath, err := ConfigDirPath()
	if err != nil {
		return "", err
	}
	return filepath.Join(configDirPath, "containers"), nil
}

func ServicesDockerComposeFilePath() (string, error) {
	containersPath, err := ConfigContainersPath()
	if err != nil {
		return "", err
	}
	return filepath.Join(containersPath, "docker-compose.yml"), nil
}

func HAProxyConfigFilePath() (string, error) {
	containersPath, err := ConfigContainersPath()
	if err != nil {
		return "", err
	}
	return filepath.Join(containersPath, "haproxy-config", HAProxyConfigFileName), nil
}
