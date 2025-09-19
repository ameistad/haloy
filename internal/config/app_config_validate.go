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
	if ac.Name == "" {
		return fmt.Errorf("app 'name' is required")
	}

	if !isValidAppName(ac.Name) {
		return fmt.Errorf("invalid app name '%s'; must contain only alphanumeric characters, hyphens, and underscores", ac.Name)
	}

	if ac.Server != "" && len(ac.Targets) > 0 {
		return fmt.Errorf("configuration cannot contain both 'server' and 'targets'; please use one method")
	}

	// If using targets, validate each merged target configuration
	if len(ac.Targets) > 0 {
		for name, overrides := range ac.Targets {
			mergedConfig := ac.MergeWithTarget(overrides)
			if err := mergedConfig.TargetConfig.Validate(format); err != nil {
				return fmt.Errorf("validation failed for target '%s': %w", name, err)
			}
		}
		// Single target with embedded TargetConfig
	} else {
		if err := ac.TargetConfig.Validate(format); err != nil {
			return err
		}
	}

	return nil
}

func (tc *TargetConfig) Validate(format string) error {
	if err := tc.Image.Validate(); err != nil {
		return fmt.Errorf("invalid image: %w", err)
	}

	if len(tc.Domains) > 0 {
		for _, domain := range tc.Domains {
			if err := domain.Validate(); err != nil {
				return err
			}
		}
	}

	if tc.ACMEEmail != "" && !helpers.IsValidEmail(tc.ACMEEmail) {
		return fmt.Errorf("%s is invalid '%s'; must be a valid email address", getFieldNameForFormat(*tc, "ACMEEmail", format), tc.ACMEEmail)
	}

	for j, envVar := range tc.Env {
		if err := envVar.Validate(format); err != nil {
			return fmt.Errorf("env[%d]: %w", j, err)
		}
	}

	for _, volume := range tc.Volumes {
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

	if tc.HealthCheckPath != "" {
		if tc.HealthCheckPath[0] != '/' {
			return fmt.Errorf("%s must start with a slash", getFieldNameForFormat(*tc, "HealthCheckPath", format))
		}
	}

	if tc.Replicas != nil {
		if int(*tc.Replicas) < 1 {
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
