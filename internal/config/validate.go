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
	// This regex enforces that labels start/end with alphanumeric chars
	// and contain only alphanumeric chars or hyphens internally.
	pattern := `^([a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?\.)+[a-zA-Z]{2,}$`
	matched, err := regexp.MatchString(pattern, domain)
	if err != nil {
		// Consider logging the regex compilation error if it happens
		return fmt.Errorf("domain regex error: %w", err)
	}
	if !matched {
		// Add checks for other potential issues not covered by regex, if needed
		if strings.HasPrefix(domain, "-") || strings.Contains(domain, ".-") {
			return fmt.Errorf("invalid domain format: labels cannot start with a hyphen: %s", domain)
		}
		// Add more specific checks if the regex fails for unexpected reasons
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

func (ac *AppConfig) Validate() error {
	if ac.Name == "" {
		return errors.New("name cannot be empty")
	}

	if err := ac.Source.Validate(); err != nil {
		return fmt.Errorf("invalid source: %w", err)
	}
	if len(ac.Domains) == 0 {
		return fmt.Errorf("no domains defined")
	}
	for _, domain := range ac.Domains {
		if err := ValidateDomain(domain.Canonical); err != nil {
			return err
		}
		for _, alias := range domain.Aliases {
			if err := ValidateDomain(alias); err != nil {
				return fmt.Errorf("alias '%s': %w", alias, err)
			}
		}
	}

	// Validate ACME email.
	if ac.ACMEEmail == "" {
		return fmt.Errorf("missing ACME email used to get TLS certificates")
	}
	if !helpers.IsValidEmail(ac.ACMEEmail) {
		return fmt.Errorf("invalid ACME email '%s'", ac.ACMEEmail)
	}

	// Validate environment variables.
	for j, envVar := range ac.Env {
		if err := envVar.Validate(); err != nil {
			return fmt.Errorf("env[%d]: %w", j, err)
		}
	}

	// Validate volumes.
	for _, volume := range ac.Volumes {
		// Expected format: /host/path:/container/path[:options]
		parts := strings.Split(volume, ":")
		if len(parts) < 2 || len(parts) > 3 {
			return fmt.Errorf("invalid volume mapping '%s'; expected '/host/path:/container/path[:options]'", volume)
		}
		// Validate host path.
		if !filepath.IsAbs(parts[0]) {
			return fmt.Errorf("volume host path '%s' in '%s' is not an absolute path", parts[0], volume)
		}
		// Validate container path.
		if !filepath.IsAbs(parts[1]) {
			return fmt.Errorf("volume container path '%s' in '%s' is not an absolute path", parts[1], volume)
		}
	}

	// Validate health check path.
	if err := ValidateHealthCheckPath(ac.HealthCheckPath); err != nil {
		return err
	}

	// Validate replicas.
	if ac.Replicas == nil {
		return errors.New("replicas cannot be nil")
	}
	if *ac.Replicas < 1 {
		return errors.New("replicas must be at least 1")
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
		err := app.Validate()
		if err != nil {
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
