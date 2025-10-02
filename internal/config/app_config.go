package config

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"github.com/ameistad/haloy/internal/constants"
	"github.com/go-viper/mapstructure/v2"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
)

type TargetConfig struct {
	Image           Image    `json:"image" yaml:"image" toml:"image"`
	Server          string   `json:"server,omitempty" yaml:"server,omitempty" toml:"server,omitempty"`
	APITokenEnv     string   `json:"apiTokenEnv,omitempty" yaml:"api_token_env,omitempty" toml:"api_token_env,omitempty"`
	Domains         []Domain `json:"domains,omitempty" yaml:"domains,omitempty" toml:"domains,omitempty"`
	ACMEEmail       string   `json:"acmeEmail,omitempty" yaml:"acme_email,omitempty" toml:"acme_email,omitempty"`
	Env             []EnvVar `json:"env,omitempty" yaml:"env,omitempty" toml:"env,omitempty"`
	HealthCheckPath string   `json:"healthCheckPath,omitempty" yaml:"health_check_path,omitempty" toml:"health_check_path,omitempty"`
	Port            Port     `json:"port,omitempty" yaml:"port,omitempty" toml:"port,omitempty"`
	Replicas        *int     `json:"replicas,omitempty" yaml:"replicas,omitempty" toml:"replicas,omitempty"`
	Volumes         []string `json:"volumes,omitempty" yaml:"volumes,omitempty" toml:"volumes,omitempty"`
	NetworkMode     string   `json:"networkMode,omitempty" yaml:"network_mode,omitempty" toml:"network_mode,omitempty"`
	PreDeploy       []string `json:"preDeploy,omitempty" yaml:"pre_deploy,omitempty" toml:"pre_deploy,omitempty"`
	PostDeploy      []string `json:"postDeploy,omitempty" yaml:"post_deploy,omitempty" toml:"post_deploy,omitempty"`
}

type AppConfig struct {
	Name string `json:"name" yaml:"name" toml:"name"`

	// This tag tells the unmarshaler to treat TargetConfig's
	// fields as if they were part of AppConfig directly.
	TargetConfig     `mapstructure:",squash" json:",inline" yaml:",inline" toml:",inline"`
	Targets          map[string]*TargetConfig `json:"targets,omitempty" yaml:"targets,omitempty" toml:"targets,omitempty"`
	SecretProviders  *SecretProviders         `json:"secretProviders,omitempty" yaml:"secret_providers,omitempty" toml:"secret_providers,omitempty"`
	GlobalPreDeploy  []string                 `json:"globalPreDeploy,omitempty" yaml:"global_pre_deploy,omitempty" toml:"global_pre_deploy,omitempty"`
	GlobalPostDeploy []string                 `json:"globalPostDeploy,omitempty" yaml:"global_post_deploy,omitempty" toml:"global_post_deploy,omitempty"`
}

// mergeWithTarget creates a new AppConfig by applying a target's overrides to the base config.
func (ac *AppConfig) MergeWithTarget(override *TargetConfig) *AppConfig {
	mergedConfig := *ac

	if override == nil {
		mergedConfig.Targets = nil
		return &mergedConfig
	}

	// Apply overrides from the target. Target values take precedence.
	if override.Image.Repository != "" {
		mergedConfig.Image.Repository = override.Image.Repository
	}
	if override.Image.Tag != "" {
		mergedConfig.Image.Tag = override.Image.Tag
	}
	if override.Image.Source != "" {
		mergedConfig.Image.Source = override.Image.Source
	}
	if override.Image.History != nil {
		mergedConfig.Image.History = override.Image.History
	}
	if override.Image.RegistryAuth != nil {
		mergedConfig.Image.RegistryAuth = override.Image.RegistryAuth
	}
	if override.Server != "" {
		mergedConfig.Server = override.Server
	}
	if override.APITokenEnv != "" {
		mergedConfig.APITokenEnv = override.APITokenEnv
	}
	if override.Domains != nil {
		mergedConfig.Domains = override.Domains
	}
	if override.ACMEEmail != "" {
		mergedConfig.ACMEEmail = override.ACMEEmail
	}
	if override.Env != nil {
		mergedConfig.Env = override.Env
	}
	if override.HealthCheckPath != "" {
		mergedConfig.HealthCheckPath = override.HealthCheckPath
	}
	if override.Port != "" {
		mergedConfig.Port = override.Port
	}
	if override.Replicas != nil {
		mergedConfig.Replicas = override.Replicas
	}
	if override.Volumes != nil {
		mergedConfig.Volumes = override.Volumes
	}
	if override.NetworkMode != "" {
		mergedConfig.NetworkMode = override.NetworkMode
	}
	if override.PreDeploy != nil {
		mergedConfig.PreDeploy = override.PreDeploy
	}
	if override.PostDeploy != nil {
		mergedConfig.PostDeploy = override.PostDeploy
	}

	// The final, merged config has no concept of targets.
	mergedConfig.Targets = nil

	return &mergedConfig
}

// Normalize will set default values which will be inherited by all targets.
func (ac *AppConfig) Normalize() {
	if ac.Image.History == nil {
		defaultCount := constants.DefaultDeploymentsToKeep
		ac.Image.History = &ImageHistory{
			Strategy: HistoryStrategyLocal,
			Count:    &defaultCount,
		}
	}
	if ac.HealthCheckPath == "" {
		ac.HealthCheckPath = constants.DefaultHealthCheckPath
	}

	if ac.Port == "" {
		ac.Port = Port(constants.DefaultContainerPort)
	}

	if ac.Replicas == nil {
		defaultReplicas := constants.DefaultReplicas
		ac.Replicas = &defaultReplicas
	}
}

// LoadAppConfig loads and validates an application configuration from a file.
// Returns:
//   - appConfig: Parsed and validated application configuration, nil on error
//   - format: Detected format ("json", "yaml", "yml", or "toml"), useful for error messages
//   - err: Any error encountered during loading, parsing, or validation
func LoadAppConfig(path string) (AppConfig, string, error) {
	configFile, err := FindConfigFile(path)
	if err != nil {
		return AppConfig{}, "", err
	}

	format, err := getConfigFormat(configFile)
	if err != nil {
		return AppConfig{}, "", err
	}

	parser, err := getConfigParser(format)
	if err != nil {
		return AppConfig{}, "", err
	}

	k := koanf.New(".")
	if err := k.Load(file.Provider(configFile), parser); err != nil {
		return AppConfig{}, "", fmt.Errorf("failed to load config file: %w", err)
	}

	configKeys := k.Keys()
	appConfigType := reflect.TypeOf(AppConfig{})

	if err := checkUnknownFields(appConfigType, configKeys, format); err != nil {
		return AppConfig{}, "", err
	}

	var appConfig AppConfig
	decoderConfig := &mapstructure.DecoderConfig{
		TagName: format,
		Result:  &appConfig,
		// This ensures that embedded structs with inline tags work properly
		Squash:     true,
		DecodeHook: portDecodeHook(),
	}

	unmarshalConf := koanf.UnmarshalConf{
		Tag:           format,
		DecoderConfig: decoderConfig,
	}

	if err := k.UnmarshalWithConf("", &appConfig, unmarshalConf); err != nil {
		return AppConfig{}, "", fmt.Errorf("failed to unmarshal config: %w", err)
	}

	appConfig.Normalize()

	if err := appConfig.Validate(format); err != nil {
		return AppConfig{}, format, err
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

// Using custom Port type so we can use both string and int for port in the config.
type Port string

func (p Port) String() string {
	return string(p)
}

func portDecodeHook() mapstructure.DecodeHookFuncType {
	return func(
		f reflect.Type,
		t reflect.Type,
		data any,
	) (any, error) {
		// Only process if target type is Port
		if t != reflect.TypeOf(Port("")) {
			return data, nil
		}

		switch v := data.(type) {
		case string:
			return Port(v), nil
		case int:
			return Port(strconv.Itoa(v)), nil
		case int64:
			return Port(strconv.FormatInt(v, 10)), nil
		case float64:
			// Handle case where YAML/JSON might parse integers as floats
			if v == float64(int(v)) {
				return Port(strconv.Itoa(int(v))), nil
			}
			return nil, fmt.Errorf("port must be an integer, got float: %v", v)
		default:
			return nil, fmt.Errorf("port must be a string or integer, got %T: %v", data, data)
		}
	}
}
