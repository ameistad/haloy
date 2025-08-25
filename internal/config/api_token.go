package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/ameistad/haloy/internal/constants"
	"github.com/joho/godotenv"
)

// TODO:
// consider removing LoadAPIToken and instead take tokenEnv as an argument in apiclient.New
// rename this file to env_files.go to just load the .env
// in the init command we'll first LoadEnvFiles and use constants.EnvVarAPIToken
// in haloy we'll get the env var from the config.

// LoadAPIToken loads the API token from environment variables or .env files
func LoadAPIToken() (string, error) {
	// First, try to load from .env files
	if err := LoadEnvFiles(); err != nil {
		// Don't fail if .env files don't exist, just continue
	}

	token := os.Getenv(constants.EnvVarAPIToken)
	if token == "" {
		return "", fmt.Errorf("API token not found. Please set %s environment variable or create a %s file", constants.EnvVarAPIToken, constants.ConfigEnvFileName)
	}

	return token, nil
}

// loadEnvFiles attempts to load .env files from various locations
func LoadEnvFiles() error {
	// Try current directory first
	if err := loadEnvFile(constants.ConfigEnvFileName); err == nil {
		return nil
	}

	// Try haloy config directory
	if configDir, err := ConfigDir(); err == nil {
		configEnvPath := filepath.Join(configDir, constants.ConfigEnvFileName)
		if err := loadEnvFile(configEnvPath); err == nil {
			return nil
		}
	}

	return fmt.Errorf("no %s file found", constants.ConfigEnvFileName)
}

// loadEnvFile loads a specific .env file if it exists
func loadEnvFile(path string) error {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return err
	}
	return godotenv.Load(path)
}

// GetConfigEnvFilePath returns the path to the .env file in the haloy config directory
func GetConfigEnvFilePath() (string, error) {
	configDir, err := ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(configDir, constants.ConfigEnvFileName), nil
}
