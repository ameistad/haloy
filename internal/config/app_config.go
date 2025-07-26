package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ameistad/haloy/internal/constants"
	"github.com/spf13/viper"
)

type AppConfig struct {
	Name              string   `yaml:"name" json:"name"`
	Image             Image    `yaml:"image" json:"image"`
	Server            string   `yaml:"server,omitempty" json:"server,omitempty"`
	Domains           []Domain `yaml:"domains,omitempty" json:"domains,omitempty"`
	ACMEEmail         string   `yaml:"acme_email,omitempty" json:"acme_email,omitempty"`
	Env               []EnvVar `yaml:"env,omitempty" json:"env,omitempty"`
	DeploymentsToKeep *int     `yaml:"deployments_to_keep,omitempty" json:"deploymentsToKeep,omitempty"`
	HealthCheckPath   string   `yaml:"health_check_path,omitempty" json:"healthCheckPath,omitempty"`
	Port              string   `yaml:"port,omitempty" json:"port,omitempty"`
	Replicas          *int     `yaml:"replicas,omitempty" json:"replicas,omitempty"`
	Volumes           []string `yaml:"volumes,omitempty" json:"volumes,omitempty"`
	NetworkMode       string   `yaml:"network_mode,omitempty" json:"networkMode,omitempty"`
}

func (ac *AppConfig) Normalize() *AppConfig {
	// Default DeploymentsToKeep to the default if not set.
	if ac.DeploymentsToKeep == nil {
		defaultMax := constants.DefaultDeploymentsToKeep
		ac.DeploymentsToKeep = &defaultMax
	}

	if ac.HealthCheckPath == "" {
		ac.HealthCheckPath = constants.DefaultHealthCheckPath
	}

	if ac.Port == "" {
		ac.Port = constants.DefaultContainerPort
	}

	if ac.Replicas == nil {
		defaultReplicas := constants.DefaultReplicas
		ac.Replicas = &defaultReplicas
	}

	if ac.Server == "" {
		ac.Server = constants.DefaultAPIServerURL
	}
	return ac
}

// Temp function to load the configuration using Viper.
func LoadAppConfig(path string) (*AppConfig, error) {
	// Find the actual config file
	configFile, err := FindConfigFile(path)
	if err != nil {
		return nil, err
	}
	v := viper.New()
	v.SetConfigFile(configFile)

	if err := v.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}
	var appConfig AppConfig
	if err := v.Unmarshal(&appConfig); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	return &appConfig, nil
}

// LoadAndValidateAppConfig loads the configuration from a file path, normalizes it, and validates it.
func LoadAndValidateAppConfig(path string) (AppConfig, error) {
	appConfig, err := LoadAppConfig(path)
	if err != nil {
		return AppConfig{}, err
	}

	appConfig = appConfig.Normalize()

	if err := appConfig.Validate(); err != nil {
		return *appConfig, fmt.Errorf("config validation failed: %w", err)
	}

	return *appConfig, nil
}

var supportedExtensions = []string{".json", ".yaml", ".yml", ".toml"}
var supportedConfigNames = []string{"haloy.json", "haloy.yaml", "haloy.yml", "haloy.toml"}

// FindConfigFile finds a haloy config file based on the given path
// It supports:
// - Full path to a config file
// - Directory containing a haloy config file
// - Relative paths
func FindConfigFile(path string) (string, error) {
	// If no path provided, use current directory
	if path == "" {
		path = "."
	}

	// Convert to absolute path
	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("failed to get absolute path: %w", err)
	}

	// Check if the path exists
	stat, err := os.Stat(absPath)
	if err != nil {
		return "", fmt.Errorf("path does not exist: %s", absPath)
	}

	// If it's a file, validate it's a supported config file
	if !stat.IsDir() {
		if isValidConfigFile(absPath) {
			return absPath, nil
		}
		return "", fmt.Errorf("file %s is not a valid haloy config file (must be .json, .yaml, .yml, or .toml)", absPath)
	}

	// If it's a directory, look for haloy config files
	for _, configName := range supportedConfigNames {
		configPath := filepath.Join(absPath, configName)
		if _, err := os.Stat(configPath); err == nil {
			return configPath, nil
		}
	}

	return "", fmt.Errorf("no haloy config file found in directory %s (looking for: %s)",
		absPath, strings.Join(supportedConfigNames, ", "))
}

// isValidConfigFile checks if a file has a supported extension and reasonable name
func isValidConfigFile(path string) bool {
	ext := filepath.Ext(path)
	base := filepath.Base(path)

	// Check if extension is supported
	for _, supportedExt := range supportedExtensions {
		if ext == supportedExt {
			// For flexibility, accept any filename with supported extension
			// but prefer haloy.* naming
			return true
		}
	}

	// Also check if it's exactly one of our preferred names
	for _, configName := range supportedConfigNames {
		if base == configName {
			return true
		}
	}

	return false
}
