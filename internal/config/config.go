package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

const (
	// DockerNetwork is the network name to which containers are attached.
	DockerNetwork = "haloy-public"

	// DefaultKeepOldContainers is the default number of old containers to keep.
	DefaultKeepOldContainers = 3

	// DefaultHealthCheckPath is the path to which the health check endpoint is bound.
	DefaultHealthCheckPath = "/"

	// DefaultContainerPort is the port on which your container serves HTTP.
	DefaultContainerPort = "80"

	ConfigFileName = "apps.yml"

	HAProxyConfigFileName = "haproxy.cfg"

	// TODO: Consider adding labelPrefix
	// LabelPreix = "haloy"
)

// Defaults to ~/.config/haloy
// If HALOY_CONFIG_PATH is set, it will use that instead.
func ConfigDirPath() (string, error) {
	// First check if HALOY_CONFIG_PATH is set.
	if envPath, ok := os.LookupEnv("HALOY_CONFIG_PATH"); ok && envPath != "" {
		// Validate that the path exists and is a directory.
		if info, err := os.Stat(envPath); err == nil && info.IsDir() {
			return envPath, nil
		}
		return "", fmt.Errorf("HALOY_CONFIG_PATH is set to '%s' but it is not a valid directory", envPath)
	}

	// Fallback to the default path.
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "haloy"), nil
}

// ConfigFilePath returns "~/.config/haloy/apps.yml".
func ConfigFilePath() (string, error) {
	configDirPath, err := ConfigDirPath()
	if err != nil {
		return "", err
	}
	return filepath.Join(configDirPath, ConfigFileName), nil
}

func ConfigContainersPath() (string, error) {
	configDirPath, err := ConfigDirPath()
	if err != nil {
		return "", err
	}
	return filepath.Join(configDirPath, "containers"), nil
}

func ServicesDockerComposeFilePath() (string, error) {
	containersPath, err := ConfigContainersPath()
	if err != nil {
		return "", err
	}
	return filepath.Join(containersPath, "docker-compose.yml"), nil
}

func HAProxyConfigFilePath() (string, error) {
	containersPath, err := ConfigContainersPath()
	if err != nil {
		return "", err
	}
	return filepath.Join(containersPath, "haproxy-config", HAProxyConfigFileName), nil
}

// Domain represents either a simple canonical domain or a mapping that includes aliases.
// When decoding a scalar, the value is assigned to the Domain field and Aliases will be empty.
type Domain struct {
	Canonical string   `yaml:"domain"`
	Aliases   []string `yaml:"aliases,omitempty"`
}

func (d *Domain) ToSlice() []string {
	return append([]string{d.Canonical}, d.Aliases...)
}

// UnmarshalYAML handles decoding a Domain from either a plain scalar or a mapping.
func (d *Domain) UnmarshalYAML(value *yaml.Node) error {
	// If the YAML node is a scalar, treat it as a simple canonical domain.
	if value.Kind == yaml.ScalarNode {
		d.Canonical = value.Value
		d.Aliases = []string{}
		return nil
	}

	// If the node is a mapping, decode it normally.
	if value.Kind == yaml.MappingNode {
		type domainAlias Domain // alias to avoid recursion
		var da domainAlias
		if err := value.Decode(&da); err != nil {
			return err
		}
		*d = Domain(da)
		// Ensure Aliases is not nil.
		if d.Aliases == nil {
			d.Aliases = []string{}
		}
		return nil
	}

	return fmt.Errorf("unexpected YAML node kind %d for Domain", value.Kind)
}

type Source struct {
	Dockerfile *DockerfileSource `yaml:"dockerfile,omitempty"`
	Image      *ImageSource      `yaml:"image,omitempty"`
}

type DockerfileSource struct {
	Path         string            `yaml:"path"`
	BuildContext string            `yaml:"buildContext"`
	BuildArgs    map[string]string `yaml:"buildArgs,omitempty"`
}

// TODO: implement this
type ImageSource struct {
	Repository string `yaml:"repository"`
	Tag        string `yaml:"tag, omitempty"`
}

// AppConfig defines the configuration for an application.
type AppConfig struct {
	Name              string            `yaml:"name"`
	Source            Source            `yaml:"source"`
	Domains           []Domain          `yaml:"domains"`
	ACMEEmail         string            `yaml:"acmeEmail"`
	Env               map[string]string `yaml:"env"`
	KeepOldContainers int               `yaml:"keepOldContainers,omitempty"`
	Volumes           []string          `yaml:"volumes,omitempty"`
	HealthCheckPath   string            `yaml:"healthCheckPath,omitempty"`
	Port              string            `yaml:"port,omitempty"`
}

// Config represents the overall configuration.
type Config struct {
	Apps []AppConfig `yaml:"apps"`
}

// NormalizeConfig sets default values for the loaded configuration.
func NormalizeConfig(conf *Config) *Config {
	normalized := *conf
	normalized.Apps = make([]AppConfig, len(conf.Apps))
	for i, app := range conf.Apps {
		normalized.Apps[i] = app

		// Default KeepOldContainers to 3 if not set.
		if app.KeepOldContainers == 0 {
			normalized.Apps[i].KeepOldContainers = DefaultKeepOldContainers
		}

		if app.HealthCheckPath == "" {
			normalized.Apps[i].HealthCheckPath = DefaultHealthCheckPath
		}

		if app.Port == "" {
			normalized.Apps[i].Port = DefaultContainerPort
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

	if err := ValidateConfigFile(normalizedConfig); err != nil {
		return nil, err
	}
	return normalizedConfig, nil
}
