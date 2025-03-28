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
		if app.Dockerfile == "" {
			return fmt.Errorf("app '%s': missing dockerfile path", app.Name)
		}
		if app.BuildContext == "" {
			return fmt.Errorf("app '%s': missing build context path", app.Name)
		}
		// Check Dockerfile.
		fileInfo, err := os.Stat(app.Dockerfile)
		if os.IsNotExist(err) {
			return fmt.Errorf("app '%s': dockerfile '%s' does not exist", app.Name, app.Dockerfile)
		} else if err != nil {
			return fmt.Errorf("app '%s': unable to check dockerfile '%s': %w", app.Name, app.Dockerfile, err)
		}
		if fileInfo.IsDir() {
			return fmt.Errorf("app '%s': dockerfile '%s' is a directory, not a file", app.Name, app.Dockerfile)
		}

		// Check BuildContext.
		ctxInfo, err := os.Stat(app.BuildContext)
		if os.IsNotExist(err) {
			return fmt.Errorf("app '%s': build context '%s' does not exist", app.Name, app.BuildContext)
		} else if err != nil {
			return fmt.Errorf("app '%s': unable to check build context '%s': %w", app.Name, app.BuildContext, err)
		}
		if !ctxInfo.IsDir() {
			return fmt.Errorf("app '%s': build context '%s' is not a directory", app.Name, app.BuildContext)
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
