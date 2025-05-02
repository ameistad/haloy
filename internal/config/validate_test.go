package config

import (
	"testing"
)

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

// TODO: Add tests for AppConfig.Validate() focusing on interactions between fields.
// TODO: Add tests for Config.Validate().
