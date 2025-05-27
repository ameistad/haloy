package config

import (
	"fmt"
	"os"
	"path/filepath"
)

// Defaults to ~/.config/haloy
// If HALOY_CONFIG_PATH is set, it will use that instead.
func ConfigDirPath() (string, error) {
	// allow overriding via env
	if envPath, ok := os.LookupEnv("HALOY_CONFIG_PATH"); ok && envPath != "" {
		return envPath, nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "haloy"), nil
}

// EnsureConfigDir makes sure the config dir exists (creates it if needed)
func EnsureConfigDir() (string, error) {
	path, err := ConfigDirPath()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(path, 0755); err != nil {
		return "", fmt.Errorf("failed to create config directory '%s': %w", path, err)
	}
	return path, nil
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

func LogsPath() (string, error) {
	containersPath, err := ConfigContainersPath()
	if err != nil {
		return "", err
	}
	return filepath.Join(containersPath, "logs"), nil
}
