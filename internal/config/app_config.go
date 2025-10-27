package config

import (
	"errors"
	"fmt"
	"reflect"
	"strconv"

	"github.com/ameistad/haloy/internal/helpers"
	"github.com/go-viper/mapstructure/v2"
)

type AppConfig struct {
	Images           map[string]*Image `json:"images,omitempty" yaml:"images,omitempty" toml:"images,omitempty"`
	TargetConfig     `mapstructure:",squash" json:",inline" yaml:",inline" toml:",inline"`
	Targets          map[string]*TargetConfig `json:"targets,omitempty" yaml:"targets,omitempty" toml:"targets,omitempty"`
	SecretProviders  *SecretProviders         `json:"secretProviders,omitempty" yaml:"secret_providers,omitempty" toml:"secret_providers,omitempty"`
	GlobalPreDeploy  []string                 `json:"globalPreDeploy,omitempty" yaml:"global_pre_deploy,omitempty" toml:"global_pre_deploy,omitempty"`
	GlobalPostDeploy []string                 `json:"globalPostDeploy,omitempty" yaml:"global_post_deploy,omitempty" toml:"global_post_deploy,omitempty"`

	// Non config fields. Not read from the config file and populated on load.
	TargetName string `json:"-" yaml:"-" toml:"-"`
	Format     string `json:"-" yaml:"-" toml:"-"`
}

type TargetConfig struct {
	// Name is the application name for this deployment.
	// In a multi-target file, if this is omitted, the map key from 'targets' is used.
	// In a single-deployment file, this is required at the top level.
	Name string `json:"name,omitempty" yaml:"name,omitempty" toml:"name,omitempty"`

	// Image can be defined inline OR reference a named image (ImageKey) from the Images map
	Image           *Image      `json:"image,omitempty" yaml:"image,omitempty" toml:"image,omitempty"`
	ImageKey        string      `json:"imageKey,omitempty" yaml:"image_key,omitempty" toml:"image_key,omitempty"`
	Server          string      `json:"server,omitempty" yaml:"server,omitempty" toml:"server,omitempty"`
	APIToken        ValueSource `json:"apiToken,omitempty" yaml:"api_token,omitempty" toml:"api_token,omitempty"`
	Domains         []Domain    `json:"domains,omitempty" yaml:"domains,omitempty" toml:"domains,omitempty"`
	ACMEEmail       string      `json:"acmeEmail,omitempty" yaml:"acme_email,omitempty" toml:"acme_email,omitempty"`
	Env             []EnvVar    `json:"env,omitempty" yaml:"env,omitempty" toml:"env,omitempty"`
	HealthCheckPath string      `json:"healthCheckPath,omitempty" yaml:"health_check_path,omitempty" toml:"health_check_path,omitempty"`
	Port            Port        `json:"port,omitempty" yaml:"port,omitempty" toml:"port,omitempty"`
	Replicas        *int        `json:"replicas,omitempty" yaml:"replicas,omitempty" toml:"replicas,omitempty"`
	Volumes         []string    `json:"volumes,omitempty" yaml:"volumes,omitempty" toml:"volumes,omitempty"`
	NetworkMode     string      `json:"networkMode,omitempty" yaml:"network_mode,omitempty" toml:"network_mode,omitempty"`
	PreDeploy       []string    `json:"preDeploy,omitempty" yaml:"pre_deploy,omitempty" toml:"pre_deploy,omitempty"`
	PostDeploy      []string    `json:"postDeploy,omitempty" yaml:"post_deploy,omitempty" toml:"post_deploy,omitempty"`

	// TODO: Is this needed in the AppConfig, we added it to TargetConfig?
	// Non config fields. Not read from the config file and populated on load.
	TargetName string `json:"-" yaml:"-" toml:"-"`
	Format     string `json:"-" yaml:"-" toml:"-"`
}

type Domain struct {
	Canonical string   `yaml:"domain" json:"domain" toml:"domain"`
	Aliases   []string `yaml:"aliases,omitempty" json:"aliases,omitempty" toml:"aliases,omitempty"`
}

func (d *Domain) Validate() error {
	if err := helpers.IsValidDomain(d.Canonical); err != nil {
		return err
	}

	for _, alias := range d.Aliases {
		if err := helpers.IsValidDomain(alias); err != nil {
			return fmt.Errorf("alias '%s': %w", alias, err)
		}
	}
	return nil
}

type EnvVar struct {
	Name        string `json:"name" yaml:"name" toml:"name"`
	ValueSource `mapstructure:",squash" json:",inline" yaml:",inline" toml:",inline"`
}

func (ev *EnvVar) Validate(format string) error {
	if ev.Name == "" {
		return errors.New("environment variable 'name' cannot be empty")
	}

	if err := ev.ValueSource.Validate(); err != nil {
		// Add context to the error returned from the embedded struct's validation.
		return fmt.Errorf("environment variable '%s': %w", ev.Name, err)
	}

	return nil
}

// Using custom Port type so we can use both string and int for port in the config.
type Port string

func (p Port) String() string {
	return string(p)
}

func PortDecodeHook() mapstructure.DecodeHookFuncType {
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
