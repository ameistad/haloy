package config

import (
	"errors"
	"fmt"
)

// EnvVar represents an environment variable that can either have a plaintext value or be backed by a secret.
type EnvVar struct {
	Name       string `yaml:"name" json:"name" toml:"name" mapstructure:"name"`
	Value      string `yaml:"value,omitempty" json:"value,omitempty" toml:"value,omitempty" mapstructure:"value,omitempty"`
	SecretName string `yaml:"secret_name,omitempty" json:"secret_name,omitempty" toml:"secret_name,omitempty" mapstructure:"secret_name,omitempty"`
}

// Validate ensures the EnvVar is correctly configured.
func (ev *EnvVar) Validate() error {
	if ev.Name == "" {
		return errors.New("environment variable name cannot be empty")
	}
	// Check that exactly one of Value or SecretName is provided
	hasValue := ev.Value != ""
	hasSecretName := ev.SecretName != ""

	if hasValue && hasSecretName {
		return fmt.Errorf("environment variable '%s': cannot provide both 'value' and 'secretName'", ev.Name)
	}
	if !hasValue && !hasSecretName {
		return fmt.Errorf("environment variable '%s': must provide either 'value' or 'secretName'", ev.Name)
	}
	return nil
}
