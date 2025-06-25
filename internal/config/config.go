package config

import (
	"fmt"
	"os"
	"reflect"

	"github.com/spf13/viper"
	"gopkg.in/yaml.v3"
)

const (
	DockerNetwork            = "haloy-public"
	DefaultDeploymentsToKeep = 5
	DefaultHealthCheckPath   = "/"
	DefaultContainerPort     = "80"
	DefaultReplicas          = 1
	ConfigFileName           = "apps.yml"
	HAProxyConfigFileName    = "haproxy.cfg"
)

type Config struct {
	Apps []AppConfig `yaml:"apps"`
}

func (c *Config) UnmarshalYAML(value *yaml.Node) error {
	// Get expected field names from struct tags
	expectedFields := ExtractYAMLFieldNames(reflect.TypeOf(*c))

	// Check for unknown fields
	if err := CheckUnknownFields(value, expectedFields, ""); err != nil {
		return err
	}

	// Use type alias to avoid infinite recursion
	type ConfigAlias Config
	var alias ConfigAlias

	// Unmarshal to the alias type
	if err := value.Decode(&alias); err != nil {
		return err
	}

	// Copy data back to original struct
	*c = Config(alias)

	return nil
}

type AppConfig struct {
	Name              string   `json:"name"`
	Image             Image    `json:"image"`
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

func (a *AppConfig) UnmarshalYAML(value *yaml.Node) error {
	// Get expected field names
	expectedFields := ExtractYAMLFieldNames(reflect.TypeOf(*a))

	// Find the app name for better error messages
	var appName string
	for i := 0; i < len(value.Content); i += 2 {
		if i+1 >= len(value.Content) {
			continue
		}
		if value.Content[i].Value == "name" {
			appName = value.Content[i+1].Value
			break
		}
	}

	// Set default context
	context := "app: "
	if appName != "" {
		context = fmt.Sprintf("app '%s': ", appName)
	}

	// Check for unknown fields
	if err := CheckUnknownFields(value, expectedFields, context); err != nil {
		return err
	}

	// Use type alias to avoid infinite recursion
	type AppConfigAlias AppConfig
	var alias AppConfigAlias

	// Unmarshal to the alias type
	if err := value.Decode(&alias); err != nil {
		return err
	}

	// Copy data back to original struct
	*a = AppConfig(alias)

	return nil
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
	return ac
}

// NormalizeConfig sets default values for the loaded configuration.
func NormalizeConfig(conf *Config) *Config {
	normalized := *conf
	normalized.Apps = make([]AppConfig, len(conf.Apps))
	for i, app := range conf.Apps {
		normalized.Apps[i] = app

		// Default DeploymentsToKeep to the default if not set.
		if app.DeploymentsToKeep == nil {
			defaultMax := DefaultDeploymentsToKeep
			normalized.Apps[i].DeploymentsToKeep = &defaultMax
		}

		if app.HealthCheckPath == "" {
			normalized.Apps[i].HealthCheckPath = DefaultHealthCheckPath
		}

		if app.Port == "" {
			normalized.Apps[i].Port = DefaultContainerPort
		}

		if app.Replicas == nil {
			defaultReplicas := DefaultReplicas
			normalized.Apps[i].Replicas = &defaultReplicas
		}
	}
	return &normalized
}

// Temp function to load the configuration using Viper.
func LoadViperAppConfig(path string) (*AppConfig, error) {
	v := viper.New()
	v.SetConfigFile(path)
	if err := v.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}
	var appConfig AppConfig
	if err := v.Unmarshal(&appConfig); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	return &appConfig, nil
}

func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file '%s': %w", path, err)
	}
	var config Config
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}
	return &config, nil
}

// LoadAndValidateConfig loads the configuration from a file, normalizes it, and validates it.
func LoadAndValidateConfig(path string) (*Config, error) {
	config, err := LoadConfig(path)
	if err != nil {
		return nil, err
	}
	normalizedConfig := NormalizeConfig(config)

	if err := normalizedConfig.Validate(); err != nil {
		return nil, err
	}
	return normalizedConfig, nil
}
