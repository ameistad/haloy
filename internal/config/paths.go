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

// If HALOY_DATA_DIR is set, it will use that instead.
func DataDir() (string, error) {
	// allow overriding via env
	if envPath, ok := os.LookupEnv("HALOY_DATA_DIR"); ok && envPath != "" {
		// Handle tilde expansion for env var
		if strings.HasPrefix(envPath, "~/") {
			home, err := os.UserHomeDir()
			if err != nil {
				return "", err
			}
			envPath = filepath.Join(home, envPath[2:])
		}
		return envPath, nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	path := filepath.Join(home, ".local", "share", "haloy")
	return path, nil
}

// ConfigDir returns the Haloy configuration directory
func ConfigDir() (string, error) {
	if envPath, ok := os.LookupEnv("HALOY_CONFIG_DIR"); ok && envPath != "" {
		if strings.HasPrefix(envPath, "~/") {
			home, err := os.UserHomeDir()
			if err != nil {
				return "", err
			}
			envPath = filepath.Join(home, envPath[2:])
		}
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

func ServicesDockerComposeFile() (string, error) {
	dataDir, err := DataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dataDir, "docker-compose.yml"), nil
}

func HAProxyConfigFilePath() (string, error) {
	dataDir, err := DataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dataDir, "haproxy-config", constants.HAProxyConfigFileName), nil
}
