package helpers

import "strings"

// SanitizeString takes a string and replaces characters unsuitable for HAProxy
// identifiers (like backend names, ACL names) with underscores.
// Allows alphanumeric characters, hyphen, and underscore.
func SanitizeString(input string) string {
	if input == "" {
		return ""
	}
	var result strings.Builder
	result.Grow(len(input)) // Pre-allocate roughly the right size

	for _, r := range input {
		// Whitelist allowed characters
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			result.WriteRune(r)
		} else {
			result.WriteRune('_') // Replace disallowed characters with underscore
		}
	}
	return result.String()
}

func SanitizeFilename(email string) string {
	result := ""
	for _, c := range email {
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' {
			result += string(c)
		} else {
			result += "_"
		}
	}
	return result
}

func SafeIDPrefix(id string) string {
	if len(id) > 12 {
		return id[:12]
	}
	return id
}
