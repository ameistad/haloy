package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ameistad/haloy/internal/constants"
	"github.com/knadh/koanf/parsers/json"
	"github.com/knadh/koanf/parsers/toml"
	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
)

type AppConfig struct {
	Name              string    `json:"name" yaml:"name" toml:"name" koanf:"name"`
	Image             Image     `json:"image" yaml:"image" toml:"image" koanf:"image"`
	Server            string    `json:"server,omitempty" yaml:"server,omitempty" toml:"server,omitempty" koanf:"server"`
	Domains           []Domain  `json:"domains,omitempty" yaml:"domains,omitempty" toml:"domains,omitempty" koanf:"domains"`
	ACMEEmail         string    `json:"acmeEmail,omitempty" yaml:"acme_email,omitempty" toml:"acme_email,omitempty" koanf:"acmeEmail"`
	Env               []EnvVar  `json:"env,omitempty" yaml:"env,omitempty" toml:"env,omitempty" koanf:"env"`
	DeploymentsToKeep *int      `json:"deploymentsToKeep,omitempty" yaml:"deployments_to_keep,omitempty" toml:"deployments_to_keep,omitempty" koanf:"deploymentsToKeep"`
	HealthCheckPath   string    `json:"healthCheckPath,omitempty" yaml:"health_check_path,omitempty" toml:"health_check_path,omitempty" koanf:"healthCheckPath"`
	Port              string    `json:"port,omitempty" yaml:"port,omitempty" toml:"port,omitempty" koanf:"port"`
	Replicas          *int      `json:"replicas,omitempty" yaml:"replicas,omitempty" toml:"replicas,omitempty" koanf:"replicas"`
	Volumes           []string  `json:"volumes,omitempty" yaml:"volumes,omitempty" toml:"volumes,omitempty" koanf:"volumes"`
	NetworkMode       string    `json:"networkMode,omitempty" yaml:"network_mode,omitempty" toml:"network_mode,omitempty" koanf:"networkMode"`
	Hooks             *AppHooks `json:"hooks,omitempty" yaml:"hooks,omitempty" toml:"hooks,omitempty" koanf:"hooks"`
}

type AppHooks struct {
	PreDeploy  []string `yaml:"pre_deploy,omitempty" json:"preDeploy,omitempty" toml:"pre_deploy,omitempty" koanf:"preDeploy"`
	PostDeploy []string `yaml:"post_deploy,omitempty" json:"postDeploy,omitempty" toml:"post_deploy,omitempty" koanf:"postDeploy"`
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

	k := koanf.New(".")

	// Determine parser based on file extension
	var parser koanf.Parser
	ext := filepath.Ext(configFile)
	switch ext {
	case ".json":
		parser = json.Parser()
	case ".yaml", ".yml":
		parser = yaml.Parser()
	case ".toml":
		parser = toml.Parser()
	default:
		return nil, fmt.Errorf("unsupported config file type: %s", ext)
	}

	// Load the config file
	if err := k.Load(file.Provider(configFile), parser); err != nil {
		return nil, fmt.Errorf("failed to load config file: %w", err)
	}

	var appConfig AppConfig
	if err := k.Unmarshal("", &appConfig); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	return &appConfig, nil
}

// LoadAndValidateAppConfig loads the configuration from a file path, normalizes it, and validates it.
func LoadAndValidateAppConfig(path string) (*AppConfig, error) {
	appConfig, err := LoadAppConfig(path)
	if err != nil {
		return nil, err
	}

	appConfig = appConfig.Normalize()

	if err := appConfig.Validate(); err != nil {
		return appConfig, fmt.Errorf("config validation failed: %w", err)
	}

	return appConfig, nil
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
