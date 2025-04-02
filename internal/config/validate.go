package config

import (
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"

	"github.com/ameistad/haloy/internal/helpers"
	"gopkg.in/yaml.v3"
)

// ValidateDomain checks that a domain string is not empty and has a basic valid structure.
func ValidateDomain(domain string) error {
	if domain == "" {
		return errors.New("domain cannot be empty")
	}
	// This regular expression is a simple validator. Adjust if needed.
	pattern := `^(?:[a-zA-Z0-9-]+\.)+[a-zA-Z]{2,}$`
	matched, err := regexp.MatchString(pattern, domain)
	if err != nil {
		return err
	}
	if !matched {
		return fmt.Errorf("invalid domain format: %s", domain)
	}
	return nil
}

// ValidateHealthCheckPath checks that a health check path is a valid URL path.
func ValidateHealthCheckPath(path string) error {
	if path == "" {
		return errors.New("health check path cannot be empty")
	}
	if path[0] != '/' {
		return errors.New("health check path must start with a slash")
	}
	return nil
}

// ValidateConfigFile checks that the Config is well-formed.
func (c *Config) Validate() error {
	// Check that at least one app is defined.
	if len(c.Apps) == 0 {
		return errors.New("config: no apps defined")
	}
	for _, app := range c.Apps {
		// Enforce a non-empty app name.
		if app.Name == "" {
			return errors.New("app '': name cannot be empty")
		}

		// Validate source with app name in error message.
		if err := app.Source.Validate(); err != nil {
			return fmt.Errorf("app '%s': invalid source: %w", app.Name, err)
		}

		// Validate domains.
		if len(app.Domains) == 0 {
			return fmt.Errorf("app '%s': no domains defined", app.Name)
		}
		for _, domain := range app.Domains {
			if err := ValidateDomain(domain.Canonical); err != nil {
				return fmt.Errorf("app '%s': %w", app.Name, err)
			}
			for _, alias := range domain.Aliases {
				if err := ValidateDomain(alias); err != nil {
					return fmt.Errorf("app '%s', alias '%s': %w", app.Name, alias, err)
				}
			}
		}

		// Validate ACME email.
		if app.ACMEEmail == "" {
			return fmt.Errorf("app '%s': missing ACME email used to get TLS certificates", app.Name)
		}
		if !helpers.IsValidEmail(app.ACMEEmail) {
			return fmt.Errorf("app '%s': invalid ACME email '%s'", app.Name, app.ACMEEmail)
		}

		// Validate environment variables.
		for j, envVar := range app.Env {
			if err := envVar.Validate(); err != nil {
				return fmt.Errorf("app '%s', env[%d]: %w", app.Name, j, err)
			}
		}

		// Validate volumes.
		for _, volume := range app.Volumes {
			// Expected format: /host/path:/container/path[:options]
			parts := strings.Split(volume, ":")
			if len(parts) < 2 || len(parts) > 3 {
				return fmt.Errorf("app '%s': invalid volume mapping '%s'; expected '/host/path:/container/path[:options]'", app.Name, volume)
			}
			// Validate host path.
			if !filepath.IsAbs(parts[0]) {
				return fmt.Errorf("app '%s': volume host path '%s' in '%s' is not an absolute path", app.Name, parts[0], volume)
			}
			// Validate container path.
			if !filepath.IsAbs(parts[1]) {
				return fmt.Errorf("app '%s': volume container path '%s' in '%s' is not an absolute path", app.Name, parts[1], volume)
			}
		}

		// Validate health check path.
		if err := ValidateHealthCheckPath(app.HealthCheckPath); err != nil {
			return fmt.Errorf("app '%s': %w", app.Name, err)
		}
	}
	return nil
}

// This is used in unmarshalling to check for unknown fields in the YAML file.
// extractYAMLFieldNames returns a map of field names from YAML struct tags
func ExtractYAMLFieldNames(t reflect.Type) map[string]bool {
	fields := make(map[string]bool)
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		tag := field.Tag.Get("yaml")
		if tag == "" || tag == "-" {
			continue
		}

		// Split on comma to handle tags like `yaml:"name,omitempty"`
		parts := strings.Split(tag, ",")
		fields[parts[0]] = true
	}
	return fields
}

// checkUnknownFields verifies no unknown fields exist in the YAML node
func CheckUnknownFields(node *yaml.Node, expectedFields map[string]bool, context string) error {
	if node.Kind != yaml.MappingNode {
		return nil
	}

	// YAML mapping nodes have key-value pairs in sequence
	for i := 0; i < len(node.Content); i += 2 {
		// Skip if we somehow have an odd number of items
		if i+1 >= len(node.Content) {
			continue
		}

		// Get the key name
		key := node.Content[i].Value

		// Check if it's a known field
		if !expectedFields[key] {
			return fmt.Errorf("%sunknown field: %s", context, key)
		}
	}

	return nil
}
