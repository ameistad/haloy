package config

import (
	"fmt"
	"reflect"

	"gopkg.in/yaml.v3"
)

type Domain struct {
	Canonical string   `yaml:"domain"`
	Aliases   []string `yaml:"aliases,omitempty"`
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
