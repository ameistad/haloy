package config

import (
	"fmt"
)

type Domain struct {
	Canonical string   `yaml:"domain" json:"domain" toml:"domain"`
	Aliases   []string `yaml:"aliases,omitempty" json:"aliases,omitempty" toml:"aliases,omitempty"`
}

func (d *Domain) ToSlice() []string {
	return append([]string{d.Canonical}, d.Aliases...)
}

func (d *Domain) Validate() error {
	if err := ValidateDomain(d.Canonical); err != nil {
		return err
	}

	for _, alias := range d.Aliases {
		if err := ValidateDomain(alias); err != nil {
			return fmt.Errorf("alias '%s': %w", alias, err)
		}
	}
	return nil
}
