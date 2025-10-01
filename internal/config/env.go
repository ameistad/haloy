package config

import (
	"errors"
	"fmt"
)

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
