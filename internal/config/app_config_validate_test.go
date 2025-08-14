package config

import (
	"fmt"
	"testing"

	"github.com/ameistad/haloy/internal/helpers"
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
		wantErr bool
	}{
		{"valid simple", "myapp", true},
		{"valid with hyphen", "my-app", true},
		{"valid with underscore", "my_app", true},
		{"valid with numbers", "app123", true},
		{"invalid with space", "my app", false},
		{"invalid with dot", "my.app", false},
		{"invalid with special char", "my@app", false},
		{"invalid empty", "", false},
		{"invalid with slash", "my/app", false},
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
		{"valid domain", "example.com", true},
		{"valid domain with subdomain", "sub.example.co.uk", true},
		{"valid domain with hyphen", "example-domain.com", true},
		{"invalid TLD too short", "example.c", false},
		{"invalid no TLD", "example", false},
		{"invalid starting with hyphen", "-example.com", false},
		{"invalid domain with space", "example domain.com", false},
		{"empty domain", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := helpers.IsValidDomain(tt.domain); (err != nil) == tt.wantErr {
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
	if err := app.Validate("yaml"); err != nil {
		t.Errorf("expected valid configuration with no domains and no ACME email; got error: %v", err)
	}
}

func TestValidateHealthCheckPath(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		wantErr bool
	}{
		{"valid root path", "/", true},
		{"valid sub path", "/healthz", true},
		{"valid path with hyphen", "/health-check", true},
		{"valid path with numbers", "/status/123", true},
		{"valid path with query", "/health?check=true", true},
		{"valid path fragment", "/health#status", true},
		{"invalid no leading slash", "health", false},
		{"invalid empty path", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := isValidHealthCheckPath(tt.path); (err != nil) == tt.wantErr {
				t.Errorf("ValidateHealthCheckPath() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
