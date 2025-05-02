package helpers

import "testing"

func TestSanitizeString(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"empty string", "", ""},
		{"alphanumeric", "abc123XYZ", "abc123XYZ"},
		{"with hyphens", "my-app-name", "my-app-name"},
		{"with underscores", "my_app_name", "my_app_name"},
		{"with dots", "my.app.name", "my_app_name"},
		{"with spaces", "my app name", "my_app_name"},
		{"mixed disallowed", "my!app@name#$", "my_app_name_"},
		{"leading/trailing disallowed", ".test-app.", "_test-app_"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := SanitizeString(tt.input); got != tt.want {
				t.Errorf("SanitizeString() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSafeIDPrefix(t *testing.T) {
	tests := []struct {
		name string
		id   string
		want string
	}{
		{"long id", "abcdef1234567890", "abcdef123456"},
		{"exact length id", "abcdef123456", "abcdef123456"},
		{"short id", "abcde", "abcde"},
		{"empty id", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := SafeIDPrefix(tt.id); got != tt.want {
				t.Errorf("SafeIDPrefix() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSanitizeFilename(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"valid filename", "my-file.txt", "my-file.txt"},
		{"with spaces", "my file.txt", "my_file.txt"},
		{"with slashes", "my/file.txt", "my_file.txt"},
		{"with colons", "my:file.txt", "my_file.txt"},
		{"with question mark", "my?file.txt", "my_file.txt"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := SanitizeFilename(tt.in); got != tt.want {
				t.Errorf("SanitizeFilename() = %v, want %v", got, tt.want)
			}
		})
	}
}
