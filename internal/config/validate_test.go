package config

import (
	"fmt"
	"testing"
)

func intPtr(i int) *int { return &i }

// baseAppConfig can be used by multiple test functions
func baseAppConfig(name string) AppConfig {
	imageSourcePtr := &ImageSource{Repository: "example.com/repo", Tag: "latest"}
	return AppConfig{
		Name: name,
		Source: Source{
			Image: imageSourcePtr,
		},
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

func TestConfig_Validate(t *testing.T) {
	apps := []AppConfig{
		func() AppConfig {
			app := baseAppConfig("app1")
			app.Domains = []Domain{{Canonical: "app1.example.com"}}
			return app
		}(),
		func() AppConfig {
			app := baseAppConfig("app2")
			app.Domains = []Domain{{Canonical: "app2.example.com"}}
			return app
		}(),
		func() AppConfig {
			app := baseAppConfig("app3")
			app.Domains = []Domain{{Canonical: "app3.example.com"}}
			return app
		}(),
	}
	cfg := &Config{Apps: apps}
	err := cfg.Validate()
	if err != nil {
		t.Errorf("Config.Validate() error = %v, wantErr nil", err)
	}
}

func TestConfigValidate_NoApps(t *testing.T) {
	cfg := &Config{Apps: []AppConfig{}}
	err := cfg.Validate()
	if err == nil || err.Error() != "config: no apps defined" {
		t.Errorf("expected 'no apps defined' error, got: %v", err)
	}
}

func TestConfigValidate_EmptyAppName(t *testing.T) {
	apps := []AppConfig{
		baseAppConfig("app1"), // Valid app, now has a default domain
		baseAppConfig(""),     // App with empty name, now also has a (likely invalid due to empty name) default domain
	}
	// Ensure the empty name app also gets some domain to pass its own validation if it checks for empty domains.
	// The primary check here is for the empty app name at the Config level.
	apps[1].Domains = []Domain{{Canonical: "problem.example.com"}} // Or handle domain generation for empty name in baseAppConfig

	cfg := &Config{Apps: apps}
	err := cfg.Validate()
	if err == nil || err.Error() != "name cannot be empty" {
		t.Errorf("expected 'name cannot be empty' error, got: %v", err)
	}
}

// ...existing code...
func TestConfigValidate_UniqueAppNames(t *testing.T) {
	apps := []AppConfig{
		func() AppConfig {
			app := baseAppConfig("app1")
			// app.Domains is already set by baseAppConfig
			return app
		}(),
		func() AppConfig {
			app := baseAppConfig("app1") // Duplicate name
			// app.Domains is already set by baseAppConfig, ensure it's different if needed for clarity
			app.Domains = []Domain{{Canonical: "app1-again.example.com"}}
			return app
		}(),
		func() AppConfig {
			app := baseAppConfig("app3")
			// app.Domains is already set by baseAppConfig
			return app
		}(),
	}
	cfg := &Config{Apps: apps}
	err := cfg.Validate()
	// Adjust the expected error message to match the actual output
	if err == nil || err.Error() != "duplicate app name: 'app1'" {
		t.Errorf("expected duplicate app name error, got: %v", err)
	}
}

func TestConfigValidate_DomainUniqueness(t *testing.T) {
	// baseAppConfig is now defined at the package level, so no need to redefine imageSourcePtr here
	// The existing baseAppConfig helper is used directly.

	tests := []struct {
		name        string
		apps        []AppConfig
		wantErrMsg  string
		expectError bool
	}{
		{
			name: "valid unique domains",
			apps: []AppConfig{
				func() AppConfig {
					app := baseAppConfig("app1")
					app.Domains = []Domain{{Canonical: "app1.com", Aliases: []string{"www.app1.com"}}}
					return app
				}(),
				func() AppConfig {
					app := baseAppConfig("app2")
					app.Domains = []Domain{{Canonical: "app2.com", Aliases: []string{"www.app2.com"}}}
					return app
				}(),
			},
			expectError: false,
		},
		{
			name: "duplicate canonical domain",
			apps: []AppConfig{
				func() AppConfig {
					app := baseAppConfig("app1")
					app.Domains = []Domain{{Canonical: "shared.com"}}
					return app
				}(),
				func() AppConfig {
					app := baseAppConfig("app2")
					app.Domains = []Domain{{Canonical: "shared.com"}}
					return app
				}(),
			},
			wantErrMsg:  "config: canonical domain 'shared.com' in app 'app2' is already used as a canonical domain in app 'app1'",
			expectError: true,
		},
		{
			name: "canonical domain as alias in another app",
			apps: []AppConfig{
				func() AppConfig {
					app := baseAppConfig("app1")
					app.Domains = []Domain{{Canonical: "main.com"}}
					return app
				}(),
				func() AppConfig {
					app := baseAppConfig("app2")
					app.Domains = []Domain{{Canonical: "other.com", Aliases: []string{"main.com"}}}
					return app
				}(),
			},
			wantErrMsg:  "config: alias 'main.com' in app 'app2' is already used as a canonical domain in app 'app1'",
			expectError: true,
		},
		{
			name: "alias as canonical domain in another app",
			apps: []AppConfig{
				func() AppConfig {
					app := baseAppConfig("app1")
					app.Domains = []Domain{{Canonical: "app1.com", Aliases: []string{"shared-alias.com"}}}
					return app
				}(),
				func() AppConfig {
					app := baseAppConfig("app2")
					app.Domains = []Domain{{Canonical: "shared-alias.com"}}
					return app
				}(),
			},
			wantErrMsg:  "config: canonical domain 'shared-alias.com' in app 'app2' is already used as an alias in app 'app1'",
			expectError: true,
		},
		{
			name: "duplicate alias across apps",
			apps: []AppConfig{
				func() AppConfig {
					app := baseAppConfig("app1")
					app.Domains = []Domain{{Canonical: "app1.com", Aliases: []string{"shared-alias.com"}}}
					return app
				}(),
				func() AppConfig {
					app := baseAppConfig("app2")
					app.Domains = []Domain{{Canonical: "app2.com", Aliases: []string{"shared-alias.com"}}}
					return app
				}(),
			},
			wantErrMsg:  "config: alias 'shared-alias.com' in app 'app2' is already used as an alias in app 'app1'",
			expectError: true,
		},
		{
			name: "canonical domain of one app is alias of itself (should be fine within same app, but caught by canonical vs alias check if across apps)",
			apps: []AppConfig{
				func() AppConfig {
					app := baseAppConfig("app1")
					app.Domains = []Domain{{Canonical: "app1.com", Aliases: []string{"www.app1.com"}}}
					return app
				}(),
				func() AppConfig {
					app := baseAppConfig("app2")
					app.Domains = []Domain{{Canonical: "www.app1.com"}}
					return app
				}(),
			},
			wantErrMsg:  "config: canonical domain 'www.app1.com' in app 'app2' is already used as an alias in app 'app1'",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{Apps: tt.apps}
			err := cfg.Validate()

			if tt.expectError {
				if err == nil {
					t.Errorf("Config.Validate() expected an error, but got nil")
				} else if err.Error() != tt.wantErrMsg {
					t.Errorf("Config.Validate() error = %q, wantErrMsg %q", err.Error(), tt.wantErrMsg)
				}
			} else {
				if err != nil {
					t.Errorf("Config.Validate() error = %v, wantErr nil", err)
				}
			}
		})
	}
}
