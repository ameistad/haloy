package config

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"

	"github.com/ameistad/haloy/internal/constants"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
)

type TargetConfig struct {
	Image             Image    `json:"image" yaml:"image" toml:"image"`
	Server            string   `json:"server,omitempty" yaml:"server,omitempty" toml:"server,omitempty"`
	APITokenEnv       string   `json:"apiTokenEnv,omitempty" yaml:"api_token_env,omitempty" toml:"api_token_env,omitempty"`
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

type AppConfig struct {
	Name string `json:"name" yaml:"name" toml:"name"`
	// This tag tells the unmarshaler to treat TargetConfig's
	// fields as if they were part of AppConfig directly.
	TargetConfig `mapstructure:",squash" json:",inline" yaml:",inline" toml:",inline"`

	Targets map[string]*TargetConfig `json:"targets,omitempty" yaml:"targets,omitempty" toml:"targets,omitempty"`
}

type DeploymentJob struct {
	TargetName string
	Config     *AppConfig
}

func (ac *AppConfig) Expand(targetFlag string) ([]DeploymentJob, error) {
	if len(ac.Targets) > 0 {
		var targetsToDeploy []string
		if targetFlag != "" {
			if _, exists := ac.Targets[targetFlag]; !exists {
				return nil, fmt.Errorf("target '%s' not found in configuration", targetFlag)
			}
			targetsToDeploy = []string{targetFlag}
		} else {
			for name := range ac.Targets {
				targetsToDeploy = append(targetsToDeploy, name)
			}
		}
		var deploymentJobs []DeploymentJob
		for _, name := range targetsToDeploy {
			overrides := ac.Targets[name]
			config := ac.mergeWithTarget(overrides)
			deploymentJobs = append(deploymentJobs, DeploymentJob{
				TargetName: name,
				Config:     config,
			})
		}

		return deploymentJobs, nil

	}
	if ac.Server != "" {
		finalConfig := *ac
		finalConfig.Targets = nil
		return []DeploymentJob{{
			TargetName: "default",
			Config:     &finalConfig,
		}}, nil
	}

	return nil, fmt.Errorf("no server or targets are defined in the configuration")
}

// mergeWithTarget creates a new AppConfig by applying a target's overrides to the base config.
func (ac *AppConfig) mergeWithTarget(override *TargetConfig) *AppConfig {
	final := *ac

	if override == nil {
		final.Targets = nil
		return &final
	}

	// Apply overrides from the target. Target values take precedence.
	if override.Image.Repository != "" {
		final.Image = override.Image
	}
	if override.Server != "" {
		final.Server = override.Server
	}
	if override.APITokenEnv != "" {
		final.APITokenEnv = override.APITokenEnv
	}
	if override.Domains != nil {
		final.Domains = override.Domains
	}
	if override.ACMEEmail != "" {
		final.ACMEEmail = override.ACMEEmail
	}
	if override.Env != nil {
		final.Env = override.Env
	}
	if override.DeploymentsToKeep != nil {
		final.DeploymentsToKeep = override.DeploymentsToKeep
	}
	if override.HealthCheckPath != "" {
		final.HealthCheckPath = override.HealthCheckPath
	}
	if override.Port != "" {
		final.Port = override.Port
	}
	if override.Replicas != nil {
		final.Replicas = override.Replicas
	}
	if override.Volumes != nil {
		final.Volumes = override.Volumes
	}
	if override.NetworkMode != "" {
		final.NetworkMode = override.NetworkMode
	}
	if override.PreDeploy != nil {
		final.PreDeploy = override.PreDeploy
	}
	if override.PostDeploy != nil {
		final.PostDeploy = override.PostDeploy
	}

	// The final, merged config has no concept of targets.
	final.Targets = nil

	return &final
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

	k := koanf.New("/")
	if err := k.Load(file.Provider(configFile), parser); err != nil {
		return nil, "", fmt.Errorf("failed to load config file: %w", err)
	}

	// Debug: Print what koanf actually loaded
	log.Printf("Koanf keys: %v", k.Keys())
	log.Printf("Image repository from koanf: %v", k.Get("image.repository"))
	log.Printf("Image data from koanf: %v", k.Get("image"))
	log.Printf("All data from koanf: %v", k.All())

	configKeys := k.Keys()
	appConfigType := reflect.TypeOf(AppConfig{})

	if err := checkUnknownFields(appConfigType, configKeys, format); err != nil {
		return nil, "", err
	}

	if err := k.UnmarshalWithConf("", &appConfig, koanf.UnmarshalConf{Tag: format}); err != nil {
		return nil, "", fmt.Errorf("failed to unmarshal config: %w", err)
	}

	if appConfig == nil {
		return nil, "", fmt.Errorf("failed to unmarshal config: %w", err)
	}

	appConfig = appConfig.Normalize()
	b, _ := json.MarshalIndent(appConfig, "", "  ")
	log.Printf("Normalized AppConfig: %s\n", b)
	if err := appConfig.Validate(format); err != nil {
		return nil, format, err
	}

	return appConfig, format, nil
}

var (
	supportedExtensions  = []string{".json", ".yaml", ".yml", ".toml"}
	supportedConfigNames = []string{"haloy.json", "haloy.yaml", "haloy.yml", "haloy.toml"}
)

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

	// If it's a file, validate it's a supported extension
	if !stat.IsDir() {
		ext := filepath.Ext(absPath)
		if !slices.Contains(supportedExtensions, ext) {
			return "", fmt.Errorf("file %s is not a valid haloy config file (must be .json, .yaml, .yml, or .toml)", absPath)
		}
		return absPath, nil
	}

	// If it's a directory, look for haloy config files
	for _, configName := range supportedConfigNames {
		configPath := filepath.Join(absPath, configName)
		if _, err := os.Stat(configPath); err == nil {
			return configPath, nil
		}
	}

	// Get the directory name for the error (use base name if path is ".")
	dirName := path
	if path == "." {
		if cwd, err := os.Getwd(); err == nil {
			dirName = filepath.Base(cwd)
		}
	}

	return "", fmt.Errorf("no haloy config file found in directory %s (looking for: %s)",
		dirName, strings.Join(supportedConfigNames, ", "))
}
