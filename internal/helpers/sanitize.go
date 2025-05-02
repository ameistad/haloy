package helpers

import "strings"

// SanitizeString takes a string and replaces characters unsuitable for HAProxy
// identifiers (like backend names, ACL names) with underscores.
// Allows alphanumeric characters, hyphen, and underscore. Consecutive disallowed
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

// SanitizeFilename takes a string (originally intended for email, but should be generic filename)
// and replaces characters potentially unsafe for filenames with underscores.
// Allows alphanumeric, hyphen, and dot. Consecutive disallowed characters are replaced by a single underscore.
func SanitizeFilename(filename string) string {
	if filename == "" {
		return ""
	}
	var result strings.Builder
	result.Grow(len(filename))
	lastCharWasUnderscore := false

	for _, r := range filename {
		// Whitelist allowed characters for filenames (alphanumeric, hyphen, dot)
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '.' {
			result.WriteRune(r)
			lastCharWasUnderscore = false
		} else {
			if !lastCharWasUnderscore {
				result.WriteRune('_')
				lastCharWasUnderscore = true
			}
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
