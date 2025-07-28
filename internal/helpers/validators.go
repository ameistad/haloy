package helpers

import (
	"fmt"
	"regexp"
	"strings"
)

func IsValidEmail(email string) bool {
	emailRegex := regexp.MustCompile(`^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$`)
	return emailRegex.MatchString(email)
}

func IsValidDomain(domain string) error {
	// Check basic requirements
	if len(domain) == 0 || len(domain) > 253 {
		return fmt.Errorf("domain length must be between 1 and 253 characters")
	}

	// Check for invalid characters at start/end
	if strings.HasPrefix(domain, ".") || strings.HasSuffix(domain, ".") {
		return fmt.Errorf("domain cannot start or end with a dot")
	}

	if strings.HasPrefix(domain, "-") || strings.HasSuffix(domain, "-") {
		return fmt.Errorf("domain cannot start or end with a hyphen")
	}

	// Split into labels and validate each
	labels := strings.Split(domain, ".")
	if len(labels) < 2 {
		return fmt.Errorf("domain must have at least two labels (e.g., example.com)")
	}

	for _, label := range labels {
		if err := validateDomainLabel(label); err != nil {
			return fmt.Errorf("invalid label '%s': %w", label, err)
		}
	}

	return nil
}

func validateDomainLabel(label string) error {
	if len(label) == 0 || len(label) > 63 {
		return fmt.Errorf("label length must be between 1 and 63 characters")
	}

	if strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
		return fmt.Errorf("label cannot start or end with hyphen")
	}

	// Check for valid characters (alphanumeric and hyphens)
	for _, r := range label {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') || r == '-') {
			return fmt.Errorf("label contains invalid character: %c", r)
		}
	}

	return nil
}
