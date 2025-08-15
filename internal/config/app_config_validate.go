package config

import (
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/ameistad/haloy/internal/helpers"
)

func (ac *AppConfig) Validate(format string) error {

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
		return fmt.Errorf("%s is invalid '%s'; must be a valid email address", getFieldNameForFormat(*ac, "ACMEEmail", format), ac.ACMEEmail)
	}

	for j, envVar := range ac.Env {
		if err := envVar.Validate(format); err != nil {
			return fmt.Errorf("env[%d]: %w", j, err)
		}
	}

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

	if ac.HealthCheckPath != "" {
		if ac.HealthCheckPath[0] != '/' {
			return fmt.Errorf("%s must start with a slash", getFieldNameForFormat(*ac, "HealthCheckPath", format))
		}
	}

	if ac.Replicas != nil {
		if int(*ac.Replicas) < 1 {
			return errors.New("replicas must be at least 1")
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
