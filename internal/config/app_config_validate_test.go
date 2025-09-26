package config

import (
	"testing"

	"github.com/ameistad/haloy/internal/helpers"
)

// baseAppConfig can be used by multiple test functions
func baseAppConfig(name string) AppConfig {
	return AppConfig{
		Name: name,
		TargetConfig: TargetConfig{ // Initialize the embedded struct by its type name
			Image: Image{
				Repository: "example.com/repo",
				Tag:        "latest",
			},
			Server: "test.server.com",
			// ... initialize other fields from TargetConfig here
		},
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
		{"invalid starts with underscore", "_my-app", false},
		{"invalid starts with hyphen", "-my-app", false},
		{"invalid starts with dot", ".my-app", false},
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
			if err := helpers.IsValidDomain(tt.domain); (err == nil) == tt.wantErr {
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
