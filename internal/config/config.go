package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/viper"
)

const (
	HaloyManagerContainerName = "haloy-manager"
	HAProxyContainerName      = "haloy-haproxy"
	DockerNetwork             = "haloy-public"
	DefaultDeploymentsToKeep  = 5
	DefaultHealthCheckPath    = "/"
	DefaultContainerPort      = "80"
	DefaultReplicas           = 1
	DefaultServer             = "http://localhost:9999"
	HAProxyConfigFileName     = "haproxy.cfg"
)

type AppConfig struct {
	Name              string   `json:"name"`
	Image             Image    `json:"image"`
	Server            string   `json:"server,omitempty"`
	Domains           []Domain `json:"domains,omitempty"`
	ACMEEmail         string   `json:"acmeEmail,omitempty"`
	Env               []EnvVar `json:"env,omitempty"`
	DeploymentsToKeep *int     `json:"deploymentsToKeep,omitempty"`
	HealthCheckPath   string   `json:"healthCheckPath,omitempty"`
	Port              string   `json:"port,omitempty"`
	Replicas          *int     `json:"replicas,omitempty"`
	Volumes           []string `json:"volumes,omitempty"`
	NetworkMode       string   `json:"networkMode,omitempty"` // defaults to "bridge"
}

func (ac *AppConfig) Normalize() *AppConfig {
	// Default DeploymentsToKeep to the default if not set.
	if ac.DeploymentsToKeep == nil {
		defaultMax := DefaultDeploymentsToKeep
		ac.DeploymentsToKeep = &defaultMax
	}

	if ac.HealthCheckPath == "" {
		ac.HealthCheckPath = DefaultHealthCheckPath
	}

	if ac.Port == "" {
		ac.Port = DefaultContainerPort
	}

	if ac.Replicas == nil {
		defaultReplicas := DefaultReplicas
		ac.Replicas = &defaultReplicas
	}

	if ac.Server == "" {
		ac.Server = DefaultServer
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
