package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/ameistad/haloy/internal/helpers"
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

func ValidateSource(source Source) error {
	sourceIsDefined := false

	// Check Dockerfile Source
	if source.Dockerfile != nil {
		sourceIsDefined = true
		dfSource := source.Dockerfile
		if dfSource.Path == "" {
			return fmt.Errorf("source.dockerfile.path is required")
		}
		if dfSource.BuildContext == "" {
			return fmt.Errorf("source.dockerfile.buildContext is required")
		}

		// Check Dockerfile Path existence and type (should be a file)
		// Consider making paths absolute before checking, or resolving relative to config file?
		// For now, assuming paths are relative to where the app runs or absolute.
		fileInfo, err := os.Stat(dfSource.Path)
		if os.IsNotExist(err) {
			return fmt.Errorf("source.dockerfile.path '%s' does not exist", dfSource.Path)
		} else if err != nil {
			return fmt.Errorf("unable to check source.dockerfile.path '%s': %w", dfSource.Path, err)
		}
		if fileInfo.IsDir() {
			return fmt.Errorf("source.dockerfile.path '%s' is a directory, not a file", dfSource.Path)
		}

		// Check BuildContext existence and type (should be a directory)
		ctxInfo, err := os.Stat(dfSource.BuildContext)
		if os.IsNotExist(err) {
			return fmt.Errorf("source.dockerfile.buildContext '%s' does not exist", dfSource.BuildContext)
		} else if err != nil {
			return fmt.Errorf("unable to check source.dockerfile.buildContext '%s': %w", dfSource.BuildContext, err)
		}
		if !ctxInfo.IsDir() {
			return fmt.Errorf("source.dockerfile.buildContext '%s' is not a directory", dfSource.BuildContext)
		}
	}

	// Check Image Source
	if source.Image != nil {
		// Check if Dockerfile source was *also* defined (mutual exclusivity)
		if sourceIsDefined {
			return fmt.Errorf("cannot define both source.dockerfile and source.image")
		}
		sourceIsDefined = true
		imgSource := source.Image
		// Validate Image source fields
		if imgSource.Repository == "" {
			return fmt.Errorf("source.image.repository is required")
		}
		// Optional: Add regex validation for imgSource.Repository and imgSource.Tag if needed.
		// Example simple check: prevent whitespace
		if strings.ContainsAny(imgSource.Repository, " \t\n\r") {
			return fmt.Errorf("source.image.repository '%s' contains whitespace", imgSource.Repository)
		}
		if strings.ContainsAny(imgSource.Tag, " \t\n\r") {
			return fmt.Errorf("source.image.tag '%s' contains whitespace", imgSource.Tag)
		}
	}

	// Check if *at least one* source type was defined
	if !sourceIsDefined {
		return fmt.Errorf("source must contain either 'dockerfile' or 'image'")
	}
	return nil
}

// ValidateConfigFile checks that the Config is well-formed.
func ValidateConfigFile(conf *Config) error {
	// Validate apps.
	if len(conf.Apps) == 0 {
		return errors.New("no apps defined in config")
	}
	for _, app := range conf.Apps {
		if app.Name == "" {
			return errors.New("found an app with an empty name")
		}
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
		if len(app.ACMEEmail) == 0 {
			return fmt.Errorf("app '%s': missing ACME email used to get TLS certificates", app.Name)
		}
		if !helpers.IsValidEmail(app.ACMEEmail) {
			return fmt.Errorf("app '%s': invalid ACME email '%s'", app.Name, app.ACMEEmail)
		}

		// Validate sources.
		if err := ValidateSource(app.Source); err != nil {
			return fmt.Errorf("app '%s': %w", app.Name, err)
		}

		// Validate volumes.
		for _, volume := range app.Volumes {
			// Expected format: /host/path:/container/path[:options]
			parts := strings.Split(volume, ":")
			if len(parts) < 2 || len(parts) > 3 {
				return fmt.Errorf("app '%s': invalid volume mapping '%s'; expected '/host/path:/container/path[:options]'", app.Name, volume)
			}
			// Validate host path (first element).
			if !filepath.IsAbs(parts[0]) {
				return fmt.Errorf("app '%s': volume host path '%s' in '%s' is not an absolute path", app.Name, parts[0], volume)
			}
			// Validate container path (second element).
			if !filepath.IsAbs(parts[1]) {
				return fmt.Errorf("app '%s': volume container path '%s' in '%s' is not an absolute path", app.Name, parts[1], volume)
			}
		}

		// Check that the health check path is a valid URL path.
		if err := ValidateHealthCheckPath(app.HealthCheckPath); err != nil {
			return fmt.Errorf("app '%s': %w", app.Name, err)
		}
	}
	return nil
}
