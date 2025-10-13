package config

import (
	"reflect"
	"testing"

	"github.com/ameistad/haloy/internal/helpers"
)

func TestAppConfig_MergeWithTarget(t *testing.T) {
	defaultReplicas := 2
	overrideReplicas := 5
	defaultCount := 10

	baseConfig := AppConfig{
		TargetConfig: TargetConfig{
			Name: "myapp",
			Image: &Image{
				Repository: "nginx",
				Tag:        "1.20",
				Source:     ImageSourceRegistry,
			},
			Server:          "default.server.com",
			ACMEEmail:       "admin@default.com",
			HealthCheckPath: "/health",
			Port:            "8080",
			Replicas:        &defaultReplicas,
			NetworkMode:     "bridge",
			Volumes:         []string{"/host:/container"},
			PreDeploy:       []string{"echo 'pre'"},
			PostDeploy:      []string{"echo 'post'"},
		},
	}

	tests := []struct {
		name            string
		base            AppConfig
		override        *TargetConfig
		expectedName    string
		expectedServer  string
		expectedImage   Image
		expectNilTarget bool
	}{
		{
			name:            "nil override returns base config without targets",
			base:            baseConfig,
			override:        nil,
			expectedName:    "myapp",
			expectedServer:  "default.server.com",
			expectNilTarget: true,
		},
		{
			name: "override server only",
			base: baseConfig,
			override: &TargetConfig{
				Server: "override.server.com",
			},
			expectedName:   "myapp",
			expectedServer: "override.server.com",
			expectedImage:  *baseConfig.Image, // Should remain unchanged
		},
		{
			name: "override image repository and tag",
			base: baseConfig,
			override: &TargetConfig{
				Image: &Image{
					Repository: "custom-nginx",
					Tag:        "1.21",
				},
			},
			expectedName:   "myapp",
			expectedServer: "default.server.com",
			expectedImage: Image{
				Repository: "custom-nginx",
				Tag:        "1.21",
				Source:     ImageSourceRegistry, // Source should be inherited
			},
		},
		{
			name: "override all fields",
			base: baseConfig,
			override: &TargetConfig{
				Image: &Image{
					Repository: "apache",
					Tag:        "2.4",
					Source:     ImageSourceLocal,
				},
				Server:          "prod.server.com",
				ACMEEmail:       "admin@prod.com",
				HealthCheckPath: "/status",
				Port:            "9090",
				Replicas:        &overrideReplicas,
				NetworkMode:     "host",
				Volumes:         []string{"/prod/host:/prod/container"},
				PreDeploy:       []string{"echo 'prod pre'"},
				PostDeploy:      []string{"echo 'prod post'"},
			},
			expectedName:   "myapp",
			expectedServer: "prod.server.com",
			expectedImage: Image{
				Repository: "apache",
				Tag:        "2.4",
				Source:     ImageSourceLocal,
			},
		},
		{
			name: "override with image history",
			base: baseConfig,
			override: &TargetConfig{
				Image: &Image{
					History: &ImageHistory{
						Strategy: HistoryStrategyRegistry,
						Count:    &defaultCount,
						Pattern:  "v*",
					},
				},
			},
			expectedName:   "myapp",
			expectedServer: "default.server.com",
			expectedImage: Image{
				Repository: "nginx", // Base repository
				Tag:        "1.20",  // Base tag
				Source:     ImageSourceRegistry,
				History: &ImageHistory{
					Strategy: HistoryStrategyRegistry,
					Count:    &defaultCount,
					Pattern:  "v*",
				},
			},
		},
		{
			name: "override with registry auth",
			base: baseConfig,
			override: &TargetConfig{
				Image: &Image{
					RegistryAuth: &RegistryAuth{
						Server:   "private.registry.com",
						Username: ValueSource{Value: "user"},
						Password: ValueSource{Value: "pass"},
					},
				},
			},
			expectedName:   "myapp",
			expectedServer: "default.server.com",
			expectedImage: Image{
				Repository: "nginx", // Base repository
				Tag:        "1.20",  // Base tag
				Source:     ImageSourceRegistry,
				RegistryAuth: &RegistryAuth{
					Server:   "private.registry.com",
					Username: ValueSource{Value: "user"},
					Password: ValueSource{Value: "pass"},
				},
			},
		},
		{
			name: "override with domains",
			base: baseConfig,
			override: &TargetConfig{
				Domains: []Domain{
					{Canonical: "prod.example.com", Aliases: []string{"www.prod.example.com"}},
				},
			},
			expectedName:   "myapp",
			expectedServer: "default.server.com",
		},
		{
			name: "override with env vars",
			base: baseConfig,
			override: &TargetConfig{
				Env: []EnvVar{
					{Name: "ENV", ValueSource: ValueSource{Value: "production"}},
				},
			},
			expectedName:   "myapp",
			expectedServer: "default.server.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, _ := tt.base.MergeWithTarget("test-target", tt.override)

			if result.Name != tt.expectedName {
				t.Errorf("MergeWithTarget() Name = %s, expected %s", result.Name, tt.expectedName)
			}

			if result.Server != tt.expectedServer {
				t.Errorf("MergeWithTarget() Server = %s, expected %s", result.Server, tt.expectedServer)
			}

			if tt.expectNilTarget && result.Targets != nil {
				t.Errorf("MergeWithTarget() Targets should be nil when override is nil")
			}

			if tt.expectedImage.Repository != "" {
				if result.Image.Repository != tt.expectedImage.Repository {
					t.Errorf("MergeWithTarget() Image.Repository = %s, expected %s",
						result.Image.Repository, tt.expectedImage.Repository)
				}
				if result.Image.Tag != tt.expectedImage.Tag {
					t.Errorf("MergeWithTarget() Image.Tag = %s, expected %s",
						result.Image.Tag, tt.expectedImage.Tag)
				}
				if result.Image.Source != tt.expectedImage.Source {
					t.Errorf("MergeWithTarget() Image.Source = %s, expected %s",
						result.Image.Source, tt.expectedImage.Source)
				}
				if tt.expectedImage.History != nil {
					if result.Image.History == nil {
						t.Errorf("MergeWithTarget() Image.History should not be nil")
					} else {
						if result.Image.History.Strategy != tt.expectedImage.History.Strategy {
							t.Errorf("MergeWithTarget() Image.History.Strategy = %s, expected %s",
								result.Image.History.Strategy, tt.expectedImage.History.Strategy)
						}
					}
				}
				if tt.expectedImage.RegistryAuth != nil {
					if result.Image.RegistryAuth == nil {
						t.Errorf("MergeWithTarget() Image.RegistryAuth should not be nil")
					} else {
						if result.Image.RegistryAuth.Server != tt.expectedImage.RegistryAuth.Server {
							t.Errorf("MergeWithTarget() Image.RegistryAuth.Server = %s, expected %s",
								result.Image.RegistryAuth.Server, tt.expectedImage.RegistryAuth.Server)
						}
					}
				}
			}
		})
	}
}

func TestAppConfig_Normalize(t *testing.T) {
	tests := []struct {
		name     string
		config   AppConfig
		expected AppConfig
	}{
		{
			name: "empty config gets defaults",
			config: AppConfig{
				TargetConfig: TargetConfig{
					Name: "myapp",
				},
			},
			expected: AppConfig{
				TargetConfig: TargetConfig{
					Name:            "myapp",
					HealthCheckPath: "/",       // Default from constants
					Port:            "8080",    // Default from constants
					Replicas:        intPtr(1), // Default from constants
				},
			},
		},
		{
			name: "config with existing values keeps them",
			config: AppConfig{
				TargetConfig: TargetConfig{
					Name:            "myapp",
					HealthCheckPath: "/custom-health",
					Port:            "9090",
					Replicas:        intPtr(3),
				},
			},
			expected: AppConfig{
				TargetConfig: TargetConfig{
					Name:            "myapp",
					HealthCheckPath: "/custom-health",
					Port:            "9090",
					Replicas:        intPtr(3),
				},
			},
		},
		{
			name: "config with image history keeps it",
			config: AppConfig{
				TargetConfig: TargetConfig{
					Name: "myapp",
					Image: &Image{
						History: &ImageHistory{
							Strategy: HistoryStrategyLocal,
							Count:    intPtr(5),
						},
					},
				},
			},
			expected: AppConfig{
				TargetConfig: TargetConfig{
					Name:            "myapp",
					HealthCheckPath: "/",
					Port:            "8080",
					Replicas:        intPtr(1),
					Image: &Image{
						History: &ImageHistory{
							Strategy: HistoryStrategyLocal,
							Count:    intPtr(5),
						},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.config.Normalize()

			if tt.config.TargetConfig.HealthCheckPath != tt.expected.TargetConfig.HealthCheckPath {
				t.Errorf("Normalize() HealthCheckPath = %s, expected %s",
					tt.config.TargetConfig.HealthCheckPath, tt.expected.TargetConfig.HealthCheckPath)
			}

			if tt.config.TargetConfig.Port != tt.expected.TargetConfig.Port {
				t.Errorf("Normalize() Port = %s, expected %s",
					tt.config.TargetConfig.Port, tt.expected.TargetConfig.Port)
			}

			if tt.config.TargetConfig.Replicas == nil || *tt.config.TargetConfig.Replicas != *tt.expected.TargetConfig.Replicas {
				replicas := 0
				if tt.config.TargetConfig.Replicas != nil {
					replicas = *tt.config.TargetConfig.Replicas
				}
				expectedReplicas := 0
				if tt.expected.TargetConfig.Replicas != nil {
					expectedReplicas = *tt.expected.TargetConfig.Replicas
				}
				t.Errorf("Normalize() Replicas = %d, expected %d", replicas, expectedReplicas)
			}

			if tt.expected.TargetConfig.Image != nil && tt.expected.TargetConfig.Image.History != nil {
				if tt.config.TargetConfig.Image == nil || tt.config.TargetConfig.Image.History == nil {
					t.Errorf("Normalize() Image.History should not be nil")
				} else {
					if tt.config.TargetConfig.Image.History.Strategy != tt.expected.TargetConfig.Image.History.Strategy {
						t.Errorf("Normalize() Image.History.Strategy = %s, expected %s",
							tt.config.TargetConfig.Image.History.Strategy, tt.expected.TargetConfig.Image.History.Strategy)
					}
				}
			}
		})
	}
}

// baseAppConfig can be used by multiple test functions
func baseAppConfig(name string) AppConfig {
	return AppConfig{
		TargetConfig: TargetConfig{ // Initialize the embedded struct by its type name
			Name: name,
			Image: &Image{
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

func TestAppConfig_Validate_Comprehensive(t *testing.T) {
	tests := []struct {
		name        string
		config      AppConfig
		format      string
		expectError bool
		errMsg      string
	}{
		{
			name: "valid minimal config",
			config: AppConfig{
				TargetConfig: TargetConfig{
					Name: "myapp",
				},
				Targets: map[string]*TargetConfig{
					"prod": {
						Image: &Image{
							Repository: "nginx",
							Tag:        "latest",
						},
						Server: "prod.server.com",
					},
				},
			},
			format:      "yaml",
			expectError: false,
		},
		{
			name: "invalid target config",
			config: AppConfig{
				TargetConfig: TargetConfig{
					Name: "myapp",
				},
				Targets: map[string]*TargetConfig{
					"prod": {
						Image: &Image{
							Tag: "latest", // Missing repository
						},
						Server: "prod.server.com",
					},
				},
			},
			format:      "yaml",
			expectError: true,
			errMsg:      "validation failed for target 'prod'",
		},
		{
			name: "valid config with all fields",
			config: AppConfig{
				TargetConfig: TargetConfig{
					Name: "myapp",
					Image: &Image{
						Repository: "nginx",
						Tag:        "1.21",
						Source:     ImageSourceRegistry,
					},
					Server:          "server.com",
					ACMEEmail:       "admin@example.com",
					HealthCheckPath: "/health",
					Port:            "8080",
					Replicas:        intPtr(2),
					NetworkMode:     "bridge",
					Volumes:         []string{"/host:/container"},
					PreDeploy:       []string{"echo pre"},
					PostDeploy:      []string{"echo post"},
					Env: []EnvVar{
						{
							Name:        "ENV_VAR",
							ValueSource: ValueSource{Value: "value"},
						},
					},
					Domains: []Domain{
						{Canonical: "example.com", Aliases: []string{"www.example.com"}},
					},
				},
			},
			format:      "yaml",
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate(tt.format)
			if tt.expectError {
				if err == nil {
					t.Errorf("Validate() expected error but got none")
				} else if tt.errMsg != "" && !contains(err.Error(), tt.errMsg) {
					t.Errorf("Validate() error = %v, expected to contain %v", err, tt.errMsg)
				}
			} else {
				if err != nil {
					t.Errorf("Validate() unexpected error = %v", err)
				}
			}
		})
	}
}

func TestTargetConfig_Validate_Comprehensive(t *testing.T) {
	tests := []struct {
		name        string
		target      TargetConfig
		format      string
		expectError bool
		errMsg      string
	}{
		{
			name: "valid minimal target config",
			target: TargetConfig{
				Name: "test-app",
				Image: &Image{
					Repository: "nginx",
					Tag:        "latest",
				},
			},
			format:      "yaml",
			expectError: false,
		},
		{
			name: "valid target config with all fields",
			target: TargetConfig{
				Name: "test-app",
				Image: &Image{
					Repository: "nginx",
					Tag:        "1.21",
					Source:     ImageSourceRegistry,
				},
				Server:          "server.com",
				ACMEEmail:       "admin@example.com",
				HealthCheckPath: "/health",
				Port:            "8080",
				Replicas:        intPtr(2),
				NetworkMode:     "bridge",
				Volumes:         []string{"/host:/container"},
				PreDeploy:       []string{"echo pre"},
				PostDeploy:      []string{"echo post"},
				Env: []EnvVar{
					{
						Name:        "ENV_VAR",
						ValueSource: ValueSource{Value: "value"},
					},
				},
				Domains: []Domain{
					{Canonical: "example.com", Aliases: []string{"www.example.com"}},
				},
			},
			format:      "yaml",
			expectError: false,
		},
		{
			name: "invalid image - missing repository",
			target: TargetConfig{
				Name: "test-app",
				Image: &Image{
					Tag: "latest",
				},
			},
			format:      "yaml",
			expectError: true,
			errMsg:      "image.repository is required",
		},
		{
			name: "invalid ACME email",
			target: TargetConfig{
				Name: "test-app",
				Image: &Image{
					Repository: "nginx",
					Tag:        "latest",
				},
				ACMEEmail: "not-an-email",
			},
			format:      "yaml",
			expectError: true,
			errMsg:      "must be a valid email address",
		},
		{
			name: "invalid domain",
			target: TargetConfig{
				Name: "test-app",
				Image: &Image{
					Repository: "nginx",
					Tag:        "latest",
				},
				Domains: []Domain{
					{Canonical: "invalid domain"},
				},
			},
			format:      "yaml",
			expectError: true,
			errMsg:      "domain must have at least two labels",
		},
		{
			name: "invalid env var",
			target: TargetConfig{
				Name: "test-app",
				Image: &Image{
					Repository: "nginx",
					Tag:        "latest",
				},
				Env: []EnvVar{
					{
						Name:        "",
						ValueSource: ValueSource{Value: "value"},
					},
				},
			},
			format:      "yaml",
			expectError: true,
			errMsg:      "environment variable 'name' cannot be empty",
		},
		{
			name: "invalid volume mapping",
			target: TargetConfig{
				Name: "test-app",
				Image: &Image{
					Repository: "nginx",
					Tag:        "latest",
				},
				Volumes: []string{"invalid-volume-format"},
			},
			format:      "yaml",
			expectError: true,
			errMsg:      "invalid volume mapping",
		},
		{
			name: "invalid health check path",
			target: TargetConfig{
				Name: "test-app",
				Image: &Image{
					Repository: "nginx",
					Tag:        "latest",
				},
				HealthCheckPath: "no-leading-slash",
			},
			format:      "yaml",
			expectError: true,
			errMsg:      "must start with a slash",
		},
		{
			name: "invalid replicas",
			target: TargetConfig{
				Name: "test-app",
				Image: &Image{
					Repository: "nginx",
					Tag:        "latest",
				},
				Replicas: intPtr(0),
			},
			format:      "yaml",
			expectError: true,
			errMsg:      "replicas must be at least 1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.target.Validate(tt.format)
			if tt.expectError {
				if err == nil {
					t.Errorf("Validate() expected error but got none")
				} else if tt.errMsg != "" && !contains(err.Error(), tt.errMsg) {
					t.Errorf("Validate() error = %v, expected to contain %v", err, tt.errMsg)
				}
			} else {
				if err != nil {
					t.Errorf("Validate() unexpected error = %v", err)
				}
			}
		})
	}
}

func intPtr(i int) *int {
	return &i
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) && (s[:len(substr)] == substr || s[len(s)-len(substr):] == substr || containsSubstring(s, substr)))
}

func containsSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func TestDomain_Validate(t *testing.T) {
	tests := []struct {
		name    string
		domain  Domain
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid domain with no aliases",
			domain: Domain{
				Canonical: "example.com",
				Aliases:   []string{},
			},
			wantErr: false,
		},
		{
			name: "valid domain with valid aliases",
			domain: Domain{
				Canonical: "example.com",
				Aliases:   []string{"www.example.com", "api.example.com"},
			},
			wantErr: false,
		},
		{
			name: "invalid canonical domain",
			domain: Domain{
				Canonical: "invalid domain",
				Aliases:   []string{},
			},
			wantErr: true,
			errMsg:  "domain must have at least two labels",
		},
		{
			name: "invalid alias domain",
			domain: Domain{
				Canonical: "example.com",
				Aliases:   []string{"valid.com", "invalid domain"},
			},
			wantErr: true,
			errMsg:  "alias 'invalid domain'",
		},
		{
			name: "empty canonical domain",
			domain: Domain{
				Canonical: "",
				Aliases:   []string{},
			},
			wantErr: true,
			errMsg:  "domain length must be between 1 and 253 characters",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.domain.Validate()
			if tt.wantErr {
				if err == nil {
					t.Errorf("Validate() expected error but got none")
				} else if tt.errMsg != "" && !findInString(err.Error(), tt.errMsg) {
					t.Errorf("Validate() error = %v, expected to contain %v", err, tt.errMsg)
				}
			} else {
				if err != nil {
					t.Errorf("Validate() unexpected error = %v", err)
				}
			}
		})
	}
}

func findInString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func TestEnvVar_Validate(t *testing.T) {
	tests := []struct {
		name    string
		envVar  EnvVar
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid env var with value",
			envVar: EnvVar{
				Name: "DATABASE_URL",
				ValueSource: ValueSource{
					Value: "postgres://localhost:5432/mydb",
				},
			},
			wantErr: false,
		},
		{
			name: "valid env var with from reference",
			envVar: EnvVar{
				Name: "API_KEY",
				ValueSource: ValueSource{
					From: &SourceReference{
						Env: "API_KEY_ENV",
					},
				},
			},
			wantErr: false,
		},
		{
			name: "empty env var name",
			envVar: EnvVar{
				Name: "",
				ValueSource: ValueSource{
					Value: "some-value",
				},
			},
			wantErr: true,
			errMsg:  "environment variable 'name' cannot be empty",
		},
		{
			name: "invalid value source - both value and from",
			envVar: EnvVar{
				Name: "INVALID_VAR",
				ValueSource: ValueSource{
					Value: "direct-value",
					From: &SourceReference{
						Env: "ENV_VAR",
					},
				},
			},
			wantErr: true,
			errMsg:  "cannot provide both 'value' and 'from'",
		},
		{
			name: "invalid value source - neither value nor from",
			envVar: EnvVar{
				Name:        "INVALID_VAR",
				ValueSource: ValueSource{},
			},
			wantErr: true,
			errMsg:  "must provide either 'value' or 'from'",
		},
		{
			name: "invalid from reference - both env and secret",
			envVar: EnvVar{
				Name: "INVALID_VAR",
				ValueSource: ValueSource{
					From: &SourceReference{
						Env:    "ENV_VAR",
						Secret: "secret-key",
					},
				},
			},
			wantErr: true,
			errMsg:  "only one source reference",
		},
		{
			name: "invalid from reference - neither env nor secret",
			envVar: EnvVar{
				Name: "INVALID_VAR",
				ValueSource: ValueSource{
					From: &SourceReference{},
				},
			},
			wantErr: true,
			errMsg:  "a source reference (e.g., 'env' or 'secret') must be specified",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.envVar.Validate("yaml")
			if tt.wantErr {
				if err == nil {
					t.Errorf("Validate() expected error but got none")
				} else if tt.errMsg != "" && !contains(err.Error(), tt.errMsg) {
					t.Errorf("Validate() error = %v, expected to contain %v", err, tt.errMsg)
				}
			} else {
				if err != nil {
					t.Errorf("Validate() unexpected error = %v", err)
				}
			}
		})
	}
}

func TestSourceReference_Validate(t *testing.T) {
	tests := []struct {
		name    string
		ref     SourceReference
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid env reference",
			ref: SourceReference{
				Env: "DATABASE_URL",
			},
			wantErr: false,
		},
		{
			name: "valid secret reference",
			ref: SourceReference{
				Secret: "api-key",
			},
			wantErr: false,
		},
		{
			name:    "empty reference",
			ref:     SourceReference{},
			wantErr: true,
			errMsg:  "a source reference (e.g., 'env' or 'secret') must be specified",
		},
		{
			name: "both env and secret set",
			ref: SourceReference{
				Env:    "DATABASE_URL",
				Secret: "api-key",
			},
			wantErr: true,
			errMsg:  "only one source reference ('env' or 'secret') can be specified at a time",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.ref.Validate()
			if tt.wantErr {
				if err == nil {
					t.Errorf("Validate() expected error but got none")
				} else if tt.errMsg != "" && err.Error() != tt.errMsg {
					t.Errorf("Validate() error = %v, expected %v", err, tt.errMsg)
				}
			} else {
				if err != nil {
					t.Errorf("Validate() unexpected error = %v", err)
				}
			}
		})
	}
}

func TestValueSource_Validate(t *testing.T) {
	tests := []struct {
		name    string
		vs      ValueSource
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid value source",
			vs: ValueSource{
				Value: "direct-value",
			},
			wantErr: false,
		},
		{
			name: "valid from reference",
			vs: ValueSource{
				From: &SourceReference{
					Env: "ENV_VAR",
				},
			},
			wantErr: false,
		},
		{
			name:    "empty value source",
			vs:      ValueSource{},
			wantErr: true,
			errMsg:  "must provide either 'value' or 'from'",
		},
		{
			name: "both value and from set",
			vs: ValueSource{
				Value: "direct-value",
				From: &SourceReference{
					Env: "ENV_VAR",
				},
			},
			wantErr: true,
			errMsg:  "cannot provide both 'value' and 'from'",
		},
		{
			name: "invalid from reference",
			vs: ValueSource{
				From: &SourceReference{
					Env:    "ENV_VAR",
					Secret: "secret-key",
				},
			},
			wantErr: true,
			errMsg:  "invalid 'from' block",
		},
		{
			name: "valid secret from reference",
			vs: ValueSource{
				From: &SourceReference{
					Secret: "api-key",
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.vs.Validate()
			if tt.wantErr {
				if err == nil {
					t.Errorf("Validate() expected error but got none")
				} else if tt.errMsg != "" && !contains(err.Error(), tt.errMsg) {
					t.Errorf("Validate() error = %v, expected to contain %v", err, tt.errMsg)
				}
			} else {
				if err != nil {
					t.Errorf("Validate() unexpected error = %v", err)
				}
			}
		})
	}
}

func TestPortDecodeHook(t *testing.T) {
	decodeHook := PortDecodeHook()
	portType := reflect.TypeOf(Port(""))

	tests := []struct {
		name        string
		data        interface{}
		expectError bool
		expected    Port
		errMsg      string
	}{
		{
			name:     "string port",
			data:     "8080",
			expected: Port("8080"),
		},
		{
			name:     "integer port",
			data:     8080,
			expected: Port("8080"),
		},
		{
			name:     "int64 port",
			data:     int64(8080),
			expected: Port("8080"),
		},
		{
			name:     "float64 port that is integer",
			data:     8080.0,
			expected: Port("8080"),
		},
		{
			name:        "float64 port that is not integer",
			data:        8080.5,
			expectError: true,
			errMsg:      "port must be an integer, got float: 8080.5",
		},
		{
			name:        "boolean data",
			data:        true,
			expectError: true,
			errMsg:      "port must be a string or integer, got bool: true",
		},
		{
			name:        "slice data",
			data:        []string{"8080"},
			expectError: true,
			errMsg:      "port must be a string or integer, got []string: [8080]",
		},
		{
			name:     "zero integer",
			data:     0,
			expected: Port("0"),
		},
		{
			name:     "empty string",
			data:     "",
			expected: Port(""),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := decodeHook(reflect.TypeOf(tt.data), portType, tt.data)

			if tt.expectError {
				if err == nil {
					t.Errorf("expected error but got none")
				} else if tt.errMsg != "" && err.Error() != tt.errMsg {
					t.Errorf("error = %v, expected %v", err, tt.errMsg)
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error = %v", err)
				}
				if result != tt.expected {
					t.Errorf("result = %v, expected %v", result, tt.expected)
				}
			}
		})
	}
}

func TestPortDecodeHook_NonPortType(t *testing.T) {
	decodeHook := PortDecodeHook()
	stringType := reflect.TypeOf("")

	// Test that the hook returns data unchanged when target is not Port type
	data := "8080"
	result, err := decodeHook(reflect.TypeOf(data), stringType, data)
	if err != nil {
		t.Errorf("expected no error for non-Port target type, got %v", err)
	}
	if result != data {
		t.Errorf("expected data to be returned unchanged for non-Port target type, got %v", result)
	}
}
