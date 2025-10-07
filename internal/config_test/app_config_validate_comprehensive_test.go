package config_test

import (
	"testing"

	"github.com/ameistad/haloy/internal/config"
)

func TestAppConfig_Validate_Comprehensive(t *testing.T) {
	tests := []struct {
		name        string
		config      config.AppConfig
		format      string
		expectError bool
		errMsg      string
	}{
		{
			name: "valid minimal config",
			config: config.AppConfig{
				Name: "myapp",
				Targets: map[string]*config.TargetConfig{
					"prod": {
						Image: config.Image{
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
			config: config.AppConfig{
				Name: "myapp",
				Targets: map[string]*config.TargetConfig{
					"prod": {
						Image: config.Image{
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
			config: config.AppConfig{
				Name: "myapp",
				TargetConfig: config.TargetConfig{
					Image: config.Image{
						Repository: "nginx",
						Tag:        "1.21",
						Source:     config.ImageSourceRegistry,
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
					Env: []config.EnvVar{
						{
							Name:        "ENV_VAR",
							ValueSource: config.ValueSource{Value: "value"},
						},
					},
					Domains: []config.Domain{
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
		target      config.TargetConfig
		format      string
		expectError bool
		errMsg      string
	}{
		{
			name: "valid minimal target config",
			target: config.TargetConfig{
				Image: config.Image{
					Repository: "nginx",
					Tag:        "latest",
				},
			},
			format:      "yaml",
			expectError: false,
		},
		{
			name: "valid target config with all fields",
			target: config.TargetConfig{
				Image: config.Image{
					Repository: "nginx",
					Tag:        "1.21",
					Source:     config.ImageSourceRegistry,
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
				Env: []config.EnvVar{
					{
						Name:        "ENV_VAR",
						ValueSource: config.ValueSource{Value: "value"},
					},
				},
				Domains: []config.Domain{
					{Canonical: "example.com", Aliases: []string{"www.example.com"}},
				},
			},
			format:      "yaml",
			expectError: false,
		},
		{
			name: "invalid image - missing repository",
			target: config.TargetConfig{
				Image: config.Image{
					Tag: "latest",
				},
			},
			format:      "yaml",
			expectError: true,
			errMsg:      "image.repository is required",
		},
		{
			name: "invalid ACME email",
			target: config.TargetConfig{
				Image: config.Image{
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
			target: config.TargetConfig{
				Image: config.Image{
					Repository: "nginx",
					Tag:        "latest",
				},
				Domains: []config.Domain{
					{Canonical: "invalid domain"},
				},
			},
			format:      "yaml",
			expectError: true,
			errMsg:      "domain must have at least two labels",
		},
		{
			name: "invalid env var",
			target: config.TargetConfig{
				Image: config.Image{
					Repository: "nginx",
					Tag:        "latest",
				},
				Env: []config.EnvVar{
					{
						Name:        "",
						ValueSource: config.ValueSource{Value: "value"},
					},
				},
			},
			format:      "yaml",
			expectError: true,
			errMsg:      "environment variable 'name' cannot be empty",
		},
		{
			name: "invalid volume mapping",
			target: config.TargetConfig{
				Image: config.Image{
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
			target: config.TargetConfig{
				Image: config.Image{
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
			target: config.TargetConfig{
				Image: config.Image{
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
