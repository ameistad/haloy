package config

import (
	"errors"
	"fmt"

	"github.com/ameistad/haloy/internal/constants"
	"github.com/ameistad/haloy/internal/helpers"
)

type TargetConfig struct {
	Image           Image       `json:"image" yaml:"image" toml:"image"`
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
}

type AppConfig struct {
	Name string `json:"name" yaml:"name" toml:"name"`

	// Not read from the config file and populated on load.
	TargetName string `json:"-" yaml:"-" toml:"-"`
	Format     string `json:"-" yaml:"-" toml:"-"` // format of the config file (json, yaml or toml)

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
	if override.APIToken.Value != "" || override.APIToken.From != nil {
		mergedConfig.APIToken = override.APIToken
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
