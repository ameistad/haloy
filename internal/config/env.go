package config

import (
	"errors"
	"fmt"
)

// EnvVar represents an environment variable that can either have a plaintext value or be backed by a secret.
type EnvVar struct {
	Name       string `json:"name" yaml:"name" toml:"name"`
	Value      string `json:"value,omitempty" yaml:"value,omitempty" toml:"value,omitempty"`
	SecretName string `json:"secretName,omitempty" yaml:"secret_name,omitempty" toml:"secret_name,omitempty"`
}

// Validate ensures the EnvVar is correctly configured.
func (ev *EnvVar) Validate(format string) error {
	if ev.Name == "" {
		return errors.New("environment variable name cannot be empty")
	}
	// Check that exactly one of Value or SecretName is provided
	hasValue := ev.Value != ""
	hasSecretName := ev.SecretName != ""

	secretNameField := GetFieldNameForFormat(*ev, "SecretName", format) // the field uses different casing based on format.
	if hasValue && hasSecretName {
		return fmt.Errorf("environment variable '%s': cannot provide both 'value' and '%s'", ev.Name, secretNameField)
	}
	if !hasValue && !hasSecretName {
		return fmt.Errorf("environment variable '%s': must provide either 'value' or '%s'", ev.Name, secretNameField)
	}
	return nil
}
