package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/ameistad/haloy/internal/constants"
	"github.com/joho/godotenv"
)

const (
	// Default .env file locations to check
	DefaultEnvFile = ".env"
)

// LoadAPIToken loads the API token from environment variables or .env files
func LoadAPIToken() (string, error) {
	// First, try to load from .env files
	if err := loadEnvFiles(); err != nil {
		// Don't fail if .env files don't exist, just continue
	}

	// Get the token from environment
	token := os.Getenv(constants.EnvVarAPIToken)
	if token == "" {
		return "", fmt.Errorf("API token not found. Please set %s environment variable or create a .env file", constants.EnvVarAPIToken)
	}

	return token, nil
}

// loadEnvFiles attempts to load .env files from various locations
func loadEnvFiles() error {
	// Try current directory first
	if err := loadEnvFile(DefaultEnvFile); err == nil {
		return nil
	}

	// Try haloy config directory
	if configDir, err := ConfigDir(); err == nil {
		configEnvPath := filepath.Join(configDir, DefaultEnvFile)
		if err := loadEnvFile(configEnvPath); err == nil {
			return nil
		}
	}

	return fmt.Errorf("no .env file found")
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
	return filepath.Join(configDir, DefaultEnvFile), nil
}
