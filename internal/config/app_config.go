package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ameistad/haloy/internal/constants"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
)

type AppConfig struct {
	Name              string   `json:"name" yaml:"name" toml:"name"`
	Image             Image    `json:"image" yaml:"image" toml:"image"`
	Server            string   `json:"server,omitempty" yaml:"server,omitempty" toml:"server,omitempty"`
	Domains           []Domain `json:"domains,omitempty" yaml:"domains,omitempty" toml:"domains,omitempty"`
	ACMEEmail         string   `json:"acmeEmail,omitempty" yaml:"acme_email,omitempty" toml:"acme_email,omitempty"`
	Env               []EnvVar `json:"env,omitempty" yaml:"env,omitempty" toml:"env,omitempty"`
	DeploymentsToKeep *int     `json:"deploymentsToKeep,omitempty" yaml:"deployments_to_keep,omitempty" toml:"deployments_to_keep,omitempty"`
	HealthCheckPath   string   `json:"healthCheckPath,omitempty" yaml:"health_check_path,omitempty" toml:"health_check_path,omitempty"`
	Port              string   `json:"port,omitempty" yaml:"port,omitempty" toml:"port,omitempty"`
	Replicas          *int     `json:"replicas,omitempty" yaml:"replicas,omitempty" toml:"replicas,omitempty"`
	Volumes           []string `json:"volumes,omitempty" yaml:"volumes,omitempty" toml:"volumes,omitempty"`
	NetworkMode       string   `json:"networkMode,omitempty" yaml:"network_mode,omitempty" toml:"network_mode,omitempty"`
	PreDeploy         []string `json:"preDeploy,omitempty" yaml:"pre_deploy,omitempty" toml:"pre_deploy,omitempty"`
	PostDeploy        []string `json:"postDeploy,omitempty" yaml:"post_deploy,omitempty" toml:"post_deploy,omitempty"`
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

// LoadAppConfig loads and validates an application configuration from a file.
// Returns:
//   - appConfig: Parsed and validated application configuration, nil on error
//   - format: Detected format ("json", "yaml", "yml", or "toml"), useful for error messages
//   - err: Any error encountered during loading, parsing, or validation
func LoadAppConfig(path string) (appConfig *AppConfig, format string, err error) {
	configFile, err := FindConfigFile(path)
	if err != nil {
		return nil, "", err
	}

	format, err = getConfigFormat(configFile)
	if err != nil {
		return nil, "", err
	}

	parser, err := getConfigParser(format)
	if err != nil {
		return nil, "", err
	}

	k := koanf.New(".")
	if err := k.Load(file.Provider(configFile), parser); err != nil {
		return nil, "", fmt.Errorf("failed to load config file: %w", err)
	}

	configKeys := k.Keys()
	for _, key := range configKeys {
		fmt.Println("Config key:", key)
	}

	if err := k.UnmarshalWithConf("", &appConfig, koanf.UnmarshalConf{Tag: format}); err != nil {
		return nil, "", fmt.Errorf("failed to unmarshal config: %w", err)
	}

	if appConfig == nil {
		return nil, "", fmt.Errorf("failed to unmarshal config: %w", err)
	}

	appConfig = appConfig.Normalize()

	if err := appConfig.Validate(format); err != nil {
		return nil, format, fmt.Errorf("config validation failed: %w", err)
	}

	return appConfig, format, nil
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
