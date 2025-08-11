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

func (ac *AppConfig) Validate() error {

	if !isValidAppName(ac.Name) {
		return fmt.Errorf("invalid app name '%s'; must contain only alphanumeric characters, hyphens, and underscores", ac.Name)
	}

	if err := ac.Image.Validate(); err != nil {
		return fmt.Errorf("invalid image: %w", err)
	}

	if len(ac.Domains) > 0 {
		for _, domain := range ac.Domains {
			if err := domain.Validate(); err != nil {
				return err
			}
		}
	}

	if ac.ACMEEmail != "" && !helpers.IsValidEmail(ac.ACMEEmail) {
		return fmt.Errorf("invalid ACME email '%s'; must be a valid email address", ac.ACMEEmail)
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
	if ac.HealthCheckPath != "" {
		if err := isValidHealthCheckPath(ac.HealthCheckPath); err != nil {
			return err
		}
	}

	// Validate replicas.
	if ac.Replicas != nil {
		if int(*ac.Replicas) < 1 {
			return errors.New("replicas must be at least 1")
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

func isValidAppName(name string) bool {
	// Only allow alphanumeric, hyphens, and underscores
	matched, err := regexp.MatchString(`^[a-zA-Z0-9_-]+$`, name)
	if err != nil {
		// If regex fails, treat as invalid
		return true
	}
	return matched
}

// isValidHealthCheckPath checks that a health check path is a valid URL path.
func isValidHealthCheckPath(path string) error {
	if path == "" {
		return errors.New("health check path cannot be empty")
	}
	if path[0] != '/' {
		return errors.New("health check path must start with a slash")
	}
	return nil
}
