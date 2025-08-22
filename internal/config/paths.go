package config

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/ameistad/haloy/internal/constants"
)

// EnsureDir creates the directory and any necessary parents, logging an error if it fails.
func ensureDir(dirPath string) error {
	return os.MkdirAll(dirPath, constants.ModeDirPrivate)
}

// expandPath handles tilde expansion for paths
func expandPath(path string) (string, error) {
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		path = filepath.Join(home, path[2:])
	}
	return path, nil
}

// DataDir returns the Haloy data directory
// Should work for code run in containers and host filesystem.
// System install: /var/lib/haloy
// User install: ~/.local/share/haloy
func DataDir() (string, error) {
	if envPath, ok := os.LookupEnv(constants.EnvVarDataDir); ok && envPath != "" {
		return expandPath(envPath)
	}

	if !IsSystemMode() {
		return expandPath(constants.UserDataDir)
	}

	return constants.SystemDataDir, nil
}

// ManagerConfigDir returns the configuration directory for haloy manager.
// Should work for code run in containers and host filesystem.
// System install: /etc/haloy
// User install: ~/.config/haloy
func ManagerConfigDir() (string, error) {
	if envPath, ok := os.LookupEnv(constants.EnvVarConfigDir); ok && envPath != "" {
		expandedPath, err := expandPath(envPath)
		if err != nil {
			return "", err
		}
		if err := ensureDir(expandedPath); err != nil {
			return "", err
		}
		return expandedPath, nil
	}

	// Default to system mode unless explicitly disabled
	if IsSystemMode() {
		if err := ensureDir(constants.SystemConfigDir); err != nil {
			return "", err
		}
		return constants.SystemConfigDir, nil
	}

	// User mode fallback
	expandedPath, err := expandPath(constants.UserConfigDir)
	if err != nil {
		return "", err
	}
	if err := ensureDir(expandedPath); err != nil {
		return "", err
	}
	return expandedPath, nil
}

// ConfigDir returns the configuration directory for haloy
// Defaults to ~/.config/haloy
func ConfigDir() (string, error) {
	if envPath, ok := os.LookupEnv(constants.EnvVarConfigDir); ok && envPath != "" {
		envPath, err := expandPath(envPath)
		if err != nil {
			return "", err
		}

		if err := ensureDir(envPath); err != nil {
			return "", err
		}
		return envPath, nil
	}

	return expandPath(constants.UserConfigDir)
}

func HAProxyConfigFilePath() (string, error) {
	dataDir, err := DataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dataDir, constants.HAProxyConfigDir, constants.HAProxyConfigFileName), nil
}

func IsSystemMode() bool {
	// Check explicit override first
	if systemInstall := os.Getenv(constants.EnvVarSystemInstall); systemInstall != "" {
		return systemInstall == "true"
	}

	// Default to true (system mode) unless running as non-root user
	return os.Geteuid() == 0
}
