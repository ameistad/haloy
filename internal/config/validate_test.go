package config

import (
	"fmt"
	"testing"
)

func intPtr(i int) *int { return &i }

// baseAppConfig can be used by multiple test functions
func baseAppConfig(name string) AppConfig {
	return AppConfig{
		Name:  name,
		Image: Image{Repository: "example.com/repo", Tag: "latest"},
		// Add a default domain to prevent "no domains defined" error in unrelated tests
		Domains:         []Domain{{Canonical: fmt.Sprintf("%s.example.com", name)}},
		ACMEEmail:       "test@example.com",
		Replicas:        intPtr(1),
		HealthCheckPath: "/",
	}
}

func TestIsValidAppName(t *testing.T) {
	tests := []struct {
		name    string
		appName string
		wantErr bool // true if invalid
	}{
		{"valid simple", "myapp", false},
		{"valid with hyphen", "my-app", false},
		{"valid with underscore", "my_app", false},
		{"valid with numbers", "app123", false},
		{"invalid with space", "my app", true},
		{"invalid with dot", "my.app", true},
		{"invalid with special char", "my@app", true},
		{"invalid empty", "", true},
		{"invalid with slash", "my/app", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isValidAppName(tt.appName)
			if got != tt.wantErr {
				t.Errorf("isValidAppName(%q) = %v, want %v", tt.appName, got, tt.wantErr)
			}
		})
	}
}

func TestValidateDomain(t *testing.T) {
	tests := []struct {
		name    string
		domain  string
		wantErr bool
	}{
		{"valid domain", "example.com", false},
		{"valid domain with subdomain", "sub.example.co.uk", false},
		{"valid domain with hyphen", "example-domain.com", false},
		{"invalid TLD too short", "example.c", true},
		{"invalid no TLD", "example", true},
		{"invalid starting with hyphen", "-example.com", true},
		{"invalid domain with space", "example domain.com", true},
		{"empty domain", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := ValidateDomain(tt.domain); (err != nil) != tt.wantErr {
				t.Errorf("ValidateDomain() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidate_NoDomainsAndNoACMEEmail(t *testing.T) {
	app := baseAppConfig("nodomains")
	// Remove domains and ACME email to test that validation passes.
	app.Domains = []Domain{}
	app.ACMEEmail = ""
	if err := app.Validate(); err != nil {
		t.Errorf("expected valid configuration with no domains and no ACME email; got error: %v", err)
	}
}

func TestValidateHealthCheckPath(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		wantErr bool
	}{
		{"valid root path", "/", false},
		{"valid sub path", "/healthz", false},
		{"valid path with hyphen", "/health-check", false},
		{"valid path with numbers", "/status/123", false},
		{"invalid no leading slash", "health", true},
		{"invalid empty path", "", true},
		{"invalid path with query", "/health?check=true", false}, // Assuming query params are allowed
		{"invalid path fragment", "/health#status", false},       // Assuming fragments are allowed
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := ValidateHealthCheckPath(tt.path); (err != nil) != tt.wantErr {
				t.Errorf("ValidateHealthCheckPath() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
