package config_test

import (
	"testing"

	"github.com/ameistad/haloy/internal/config"
)

func TestAppConfig_MergeWithTarget(t *testing.T) {
	defaultReplicas := 2
	overrideReplicas := 5
	defaultCount := 10

	baseConfig := config.AppConfig{
		Name: "myapp",
		TargetConfig: config.TargetConfig{
			Image: config.Image{
				Repository: "nginx",
				Tag:        "1.20",
				Source:     config.ImageSourceRegistry,
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
		base            config.AppConfig
		override        *config.TargetConfig
		expectedName    string
		expectedServer  string
		expectedImage   config.Image
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
			override: &config.TargetConfig{
				Server: "override.server.com",
			},
			expectedName:   "myapp",
			expectedServer: "override.server.com",
			expectedImage:  baseConfig.Image, // Should remain unchanged
		},
		{
			name: "override image repository and tag",
			base: baseConfig,
			override: &config.TargetConfig{
				Image: config.Image{
					Repository: "custom-nginx",
					Tag:        "1.21",
				},
			},
			expectedName:   "myapp",
			expectedServer: "default.server.com",
			expectedImage: config.Image{
				Repository: "custom-nginx",
				Tag:        "1.21",
				Source:     config.ImageSourceRegistry, // Source should be inherited
			},
		},
		{
			name: "override all fields",
			base: baseConfig,
			override: &config.TargetConfig{
				Image: config.Image{
					Repository: "apache",
					Tag:        "2.4",
					Source:     config.ImageSourceLocal,
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
			expectedImage: config.Image{
				Repository: "apache",
				Tag:        "2.4",
				Source:     config.ImageSourceLocal,
			},
		},
		{
			name: "override with image history",
			base: baseConfig,
			override: &config.TargetConfig{
				Image: config.Image{
					History: &config.ImageHistory{
						Strategy: config.HistoryStrategyRegistry,
						Count:    &defaultCount,
						Pattern:  "v*",
					},
				},
			},
			expectedName:   "myapp",
			expectedServer: "default.server.com",
			expectedImage: config.Image{
				Repository: "nginx", // Base repository
				Tag:        "1.20",  // Base tag
				Source:     config.ImageSourceRegistry,
				History: &config.ImageHistory{
					Strategy: config.HistoryStrategyRegistry,
					Count:    &defaultCount,
					Pattern:  "v*",
				},
			},
		},
		{
			name: "override with registry auth",
			base: baseConfig,
			override: &config.TargetConfig{
				Image: config.Image{
					RegistryAuth: &config.RegistryAuth{
						Server:   "private.registry.com",
						Username: config.ValueSource{Value: "user"},
						Password: config.ValueSource{Value: "pass"},
					},
				},
			},
			expectedName:   "myapp",
			expectedServer: "default.server.com",
			expectedImage: config.Image{
				Repository: "nginx", // Base repository
				Tag:        "1.20",  // Base tag
				Source:     config.ImageSourceRegistry,
				RegistryAuth: &config.RegistryAuth{
					Server:   "private.registry.com",
					Username: config.ValueSource{Value: "user"},
					Password: config.ValueSource{Value: "pass"},
				},
			},
		},
		{
			name: "override with domains",
			base: baseConfig,
			override: &config.TargetConfig{
				Domains: []config.Domain{
					{Canonical: "prod.example.com", Aliases: []string{"www.prod.example.com"}},
				},
			},
			expectedName:   "myapp",
			expectedServer: "default.server.com",
		},
		{
			name: "override with env vars",
			base: baseConfig,
			override: &config.TargetConfig{
				Env: []config.EnvVar{
					{Name: "ENV", ValueSource: config.ValueSource{Value: "production"}},
				},
			},
			expectedName:   "myapp",
			expectedServer: "default.server.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.base.MergeWithTarget(tt.override)

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
		config   config.AppConfig
		expected config.AppConfig
	}{
		{
			name: "empty config gets defaults",
			config: config.AppConfig{
				Name: "myapp",
			},
			expected: config.AppConfig{
				Name: "myapp",
				TargetConfig: config.TargetConfig{
					HealthCheckPath: "/",       // Default from constants
					Port:            "8080",    // Default from constants
					Replicas:        intPtr(1), // Default from constants
				},
			},
		},
		{
			name: "config with existing values keeps them",
			config: config.AppConfig{
				Name: "myapp",
				TargetConfig: config.TargetConfig{
					HealthCheckPath: "/custom-health",
					Port:            "9090",
					Replicas:        intPtr(3),
				},
			},
			expected: config.AppConfig{
				Name: "myapp",
				TargetConfig: config.TargetConfig{
					HealthCheckPath: "/custom-health",
					Port:            "9090",
					Replicas:        intPtr(3),
				},
			},
		},
		{
			name: "config with image history keeps it",
			config: config.AppConfig{
				Name: "myapp",
				TargetConfig: config.TargetConfig{
					Image: config.Image{
						History: &config.ImageHistory{
							Strategy: config.HistoryStrategyLocal,
							Count:    intPtr(5),
						},
					},
				},
			},
			expected: config.AppConfig{
				Name: "myapp",
				TargetConfig: config.TargetConfig{
					HealthCheckPath: "/",
					Port:            "8080",
					Replicas:        intPtr(1),
					Image: config.Image{
						History: &config.ImageHistory{
							Strategy: config.HistoryStrategyLocal,
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

			if tt.expected.TargetConfig.Image.History != nil {
				if tt.config.TargetConfig.Image.History == nil {
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

func intPtr(i int) *int {
	return &i
}
