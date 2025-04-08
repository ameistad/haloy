package config

import (
	"fmt"
	"os"
	"reflect"

	"gopkg.in/yaml.v3"
)

const (
	// DockerNetwork is the network name to which containers are attached.
	DockerNetwork = "haloy-public"

	// DefaultMaxContainersToKeep is the default number of old containers to keep.
	DefaultMaxContainersToKeep = 3

	// DefaultHealthCheckPath is the path to which the health check endpoint is bound.
	DefaultHealthCheckPath = "/"

	// DefaultContainerPort is the port on which your container serves HTTP.
	DefaultContainerPort = "80"

	// DefaultReplicas is the default number of replicas for a container.
	DefaultReplicas = 1

	ConfigFileName = "apps.yml"

	HAProxyConfigFileName = "haproxy.cfg"

	// TODO: Consider adding labelPrefix
	// LabelPreix = "haloy"
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
	Name      string   `yaml:"name"`
	Source    Source   `yaml:"source"`
	Domains   []Domain `yaml:"domains"`
	ACMEEmail string   `yaml:"acmeEmail"`
	Env       []EnvVar `yaml:"env,omitempty"`
	// Using pointer to allow nil value
	MaxContainersToKeep *int     `yaml:"maxContainersToKeep,omitempty"`
	Volumes             []string `yaml:"volumes,omitempty"`
	HealthCheckPath     string   `yaml:"healthCheckPath,omitempty"`
	Port                string   `yaml:"port,omitempty"`
	Replicas            *int     `yaml:"replicas,omitempty"`
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

type Domain struct {
	Canonical string   `yaml:"domain"`
	Aliases   []string `yaml:"aliases,omitempty"`
}

func (d *Domain) ToSlice() []string {
	return append([]string{d.Canonical}, d.Aliases...)
}
func (d *Domain) UnmarshalYAML(value *yaml.Node) error {
	// If the YAML node is a scalar, treat it as a simple canonical domain.
	if value.Kind == yaml.ScalarNode {
		d.Canonical = value.Value
		d.Aliases = []string{}
		return nil
	}

	// If the node is a mapping, check for unknown fields
	if value.Kind == yaml.MappingNode {
		expectedFields := ExtractYAMLFieldNames(reflect.TypeOf(*d))

		if err := CheckUnknownFields(value, expectedFields, "domain: "); err != nil {
			return err
		}

		// Use type alias to avoid infinite recursion
		type DomainAlias Domain
		var alias DomainAlias

		// Unmarshal to the alias type
		if err := value.Decode(&alias); err != nil {
			return err
		}

		// Copy data back to original struct
		*d = Domain(alias)

		// Ensure Aliases is not nil
		if d.Aliases == nil {
			d.Aliases = []string{}
		}

		return nil
	}

	return fmt.Errorf("unexpected YAML node kind %d for Domain", value.Kind)
}

// NormalizeConfig sets default values for the loaded configuration.
func NormalizeConfig(conf *Config) *Config {
	normalized := *conf
	normalized.Apps = make([]AppConfig, len(conf.Apps))
	for i, app := range conf.Apps {
		normalized.Apps[i] = app

		// Default MaxContainersToKeep to 3 if not set.
		if app.MaxContainersToKeep == nil {
			defaultMax := DefaultMaxContainersToKeep
			normalized.Apps[i].MaxContainersToKeep = &defaultMax
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
