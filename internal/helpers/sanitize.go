package helpers

import "strings"

// SanitizeString takes a string and sanitizes it for use as a safe identifier.
// Suitable for HAProxy identifiers (backend names, ACL names), Docker container names,
// and filenames (when extensions are added separately).
// Allows alphanumeric characters, hyphens, and underscores. Consecutive disallowed
// characters are replaced by a single underscore.
func SanitizeString(input string) string {
	if input == "" {
		return ""
	}
	var result strings.Builder
	result.Grow(len(input))        // Pre-allocate roughly the right size
	lastCharWasUnderscore := false // Track if the last added char was a replacement underscore

	for _, r := range input {
		// Whitelist allowed characters
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			result.WriteRune(r)
			lastCharWasUnderscore = false // Reset flag if allowed char is added
		} else {
			// Only add an underscore if the previous char wasn't already a replacement underscore
			if !lastCharWasUnderscore {
				result.WriteRune('_')
				lastCharWasUnderscore = true // Set flag as we added a replacement underscore
			}
			// If lastCharWasUnderscore is true, we skip adding another underscore
		}
	}
	return result.String()
}

func SafeIDPrefix(id string) string {
	if len(id) > 12 {
		return id[:12]
	}
	return id
}
