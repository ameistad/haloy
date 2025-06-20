package config

import (
	"os"
	"path/filepath"
)

// EnsureDir creates the directory and any necessary parents, logging an error if it fails.
func ensureDir(dirPath string) error {
	return os.MkdirAll(dirPath, 0755)
}

// Defaults to ~/.config/haloy
// If HALOY_CONFIG_PATH is set, it will use that instead.
func ConfigDirPath() (string, error) {
	// allow overriding via env
	if envPath, ok := os.LookupEnv("HALOY_CONFIG_PATH"); ok && envPath != "" {
		if err := ensureDir(envPath); err != nil {
			return "", err
		}
		return envPath, nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	path := filepath.Join(home, ".config", "haloy")
	if err := ensureDir(path); err != nil {
		return "", err
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
	path := filepath.Join(configDirPath, "containers")
	if err := ensureDir(path); err != nil {
		return "", err
	}
	return path, nil
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
	// no need to ensure dir exists as it is created by the manager container.
	return filepath.Join(containersPath, "logs"), nil
}

func HistoryPath() (string, error) {
	configDirPath, err := ConfigDirPath()
	if err != nil {
		return "", err
	}
	path := filepath.Join(configDirPath, "history")
	if err := ensureDir(path); err != nil {
		return "", err
	}
	return path, nil
}
