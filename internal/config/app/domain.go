package app

import (
	"fmt"

	"github.com/ameistad/haloy/internal/helpers"
)

type Domain struct {
	Canonical string   `yaml:"domain" json:"domain" toml:"domain"`
	Aliases   []string `yaml:"aliases,omitempty" json:"aliases,omitempty" toml:"aliases,omitempty"`
}

func (d *Domain) ToSlice() []string {
	return append([]string{d.Canonical}, d.Aliases...)
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
