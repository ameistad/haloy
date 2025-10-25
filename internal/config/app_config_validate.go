package config

import (
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/ameistad/haloy/internal/helpers"
)

func (ac *AppConfig) isEmpty() bool {
	return ac.Name == "" &&
		(ac.Image == nil || ac.Image.Repository == "") &&
		ac.Server == "" &&
		len(ac.Domains) == 0 &&
		len(ac.Env) == 0 &&
		len(ac.Targets) == 0
}

func (ac *AppConfig) Validate(format string) error {
	// We'll assume yaml if no format is supplied.
	if format == "" {
		format = "yaml"
	}

	if ac.isEmpty() {
		return fmt.Errorf("app configuration is required")
	}

	isMultiTarget := len(ac.Targets) > 0

	if isMultiTarget {
		for targetName, overrides := range ac.Targets {
			mergedConfig, err := ac.ResolveTarget(targetName, overrides)
			if err != nil {
				return fmt.Errorf("unable to resolve config for target '%s': %w", targetName, err)
			}
			if err := mergedConfig.TargetConfig.Validate(format); err != nil {
				return fmt.Errorf("validation failed for target '%s': %w", targetName, err)
			}
		}
	} else {
		if ac.Image == nil {
			return fmt.Errorf("image is required for single-target configuration")
		}
		if err := ac.TargetConfig.Validate(format); err != nil {
			return err
		}
	}

	return nil
}

func (tc *TargetConfig) Validate(format string) error {
	if tc.Name == "" {
		return errors.New("app 'name' is required")
	}

	if tc.Server == "" {
		return errors.New("server is required")
	}

	if !isValidAppName(tc.Name) {
		return fmt.Errorf("invalid app name '%s'; must contain only alphanumeric characters, hyphens, and underscores", tc.Name)
	}

	if tc.Image != nil && tc.ImageKey != "" {
		return fmt.Errorf("cannot specify both 'image' and 'imageRef' in target config")
	}

	if tc.Image == nil && tc.ImageKey == "" {
		return fmt.Errorf("either 'image' or 'imageRef' must be specified")
	}

	if tc.Image != nil {
		if err := tc.Image.Validate(format); err != nil {
			return fmt.Errorf("invalid image: %w", err)
		}
	}

	if len(tc.Domains) > 0 {
		for _, domain := range tc.Domains {
			if err := domain.Validate(); err != nil {
				return err
			}
		}
	}

	if tc.ACMEEmail != "" && !helpers.IsValidEmail(tc.ACMEEmail) {
		return fmt.Errorf("%s is invalid '%s'; must be a valid email address", GetFieldNameForFormat(TargetConfig{}, "ACMEEmail", format), tc.ACMEEmail)
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
			return fmt.Errorf("%s must start with a slash", GetFieldNameForFormat(TargetConfig{}, "HealthCheckPath", format))
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
	// Must start with alphanumeric character
	// This is to satisfy docker container name restrictions
	matched, err := regexp.MatchString(`^[a-zA-Z0-9][a-zA-Z0-9_-]*$`, name)
	if err != nil {
		return false
	}
	return matched
}
