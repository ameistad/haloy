package appconfigloader

import (
	"testing"

	"github.com/ameistad/haloy/internal/config"
	"github.com/ameistad/haloy/internal/helpers"
)

func TestMergeToTarget(t *testing.T) {
	defaultReplicas := 2
	overrideReplicas := 5
	defaultCount := 10

	baseAppConfig := config.AppConfig{
		TargetConfig: config.TargetConfig{
			Name: "myapp",
			Image: &config.Image{
				Repository: "nginx",
				Tag:        "1.20",
			},
			Server:          "default.haloy.dev",
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
		appConfig       config.AppConfig
		targetConfig    config.TargetConfig
		targetName      string
		expectedName    string
		expectedServer  string
		expectedImage   config.Image
		expectNilTarget bool
	}{
		{
			name:           "empty target config inherits from base",
			appConfig:      baseAppConfig,
			targetConfig:   config.TargetConfig{},
			targetName:     "test-target",
			expectedName:   "myapp",
			expectedServer: "default.haloy.dev",
			expectedImage:  *baseAppConfig.Image,
		},
		{
			name:      "override server only",
			appConfig: baseAppConfig,
			targetConfig: config.TargetConfig{
				Server: "override.haloy.dev",
			},
			targetName:     "test-target",
			expectedName:   "myapp",
			expectedServer: "override.haloy.dev",
			expectedImage:  *baseAppConfig.Image,
		},
		{
			name:      "override image repository and tag",
			appConfig: baseAppConfig,
			targetConfig: config.TargetConfig{
				Image: &config.Image{
					Repository: "custom-nginx",
					Tag:        "1.21",
				},
			},
			targetName:     "test-target",
			expectedName:   "myapp",
			expectedServer: "default.haloy.dev",
			expectedImage: config.Image{
				Repository: "custom-nginx",
				Tag:        "1.21",
			},
		},
		{
			name:      "override all fields",
			appConfig: baseAppConfig,
			targetConfig: config.TargetConfig{
				Image: &config.Image{
					Repository: "apache",
					Tag:        "2.4",
				},
				Server:          "prod.haloy.dev",
				ACMEEmail:       "admin@prod.com",
				HealthCheckPath: "/status",
				Port:            "9090",
				Replicas:        &overrideReplicas,
				NetworkMode:     "host",
				Volumes:         []string{"/prod/host:/prod/container"},
				PreDeploy:       []string{"echo 'prod pre'"},
				PostDeploy:      []string{"echo 'prod post'"},
			},
			targetName:     "test-target",
			expectedName:   "myapp",
			expectedServer: "prod.haloy.dev",
			expectedImage: config.Image{
				Repository: "apache",
				Tag:        "2.4",
			},
		},
		{
			name:      "override with image history",
			appConfig: baseAppConfig,
			targetConfig: config.TargetConfig{
				Image: &config.Image{
					History: &config.ImageHistory{
						Strategy: config.HistoryStrategyRegistry,
						Count:    &defaultCount,
						Pattern:  "v*",
					},
				},
			},
			targetName:     "test-target",
			expectedName:   "myapp",
			expectedServer: "default.haloy.dev",
			expectedImage: config.Image{
				Repository: "nginx", // Base repository
				Tag:        "1.20",  // Base tag
				History: &config.ImageHistory{
					Strategy: config.HistoryStrategyRegistry,
					Count:    &defaultCount,
					Pattern:  "v*",
				},
			},
		},
		{
			name:      "override with registry auth",
			appConfig: baseAppConfig,
			targetConfig: config.TargetConfig{
				Image: &config.Image{
					RegistryAuth: &config.RegistryAuth{
						Server:   "private.registry.com",
						Username: config.ValueSource{Value: "user"},
						Password: config.ValueSource{Value: "pass"},
					},
				},
			},
			targetName:     "test-target",
			expectedName:   "myapp",
			expectedServer: "default.haloy.dev",
			expectedImage: config.Image{
				Repository: "nginx", // Base repository
				Tag:        "1.20",  // Base tag
				RegistryAuth: &config.RegistryAuth{
					Server:   "private.registry.com",
					Username: config.ValueSource{Value: "user"},
					Password: config.ValueSource{Value: "pass"},
				},
			},
		},
		{
			name:      "override with domains",
			appConfig: baseAppConfig,
			targetConfig: config.TargetConfig{
				Domains: []config.Domain{
					{Canonical: "prod.example.com", Aliases: []string{"www.prod.example.com"}},
				},
			},
			targetName:     "test-target",
			expectedName:   "myapp",
			expectedServer: "default.haloy.dev",
		},
		{
			name:      "override with env vars",
			appConfig: baseAppConfig,
			targetConfig: config.TargetConfig{
				Env: []config.EnvVar{
					{Name: "ENV", ValueSource: config.ValueSource{Value: "production"}},
				},
			},
			targetName:     "test-target",
			expectedName:   "myapp",
			expectedServer: "default.haloy.dev",
		},
		{
			name: "target name used when no name in base or target",
			appConfig: config.AppConfig{
				TargetConfig: config.TargetConfig{
					Image: &config.Image{
						Repository: "nginx",
						Tag:        "latest",
					},
					Server: "test.haloy.dev",
				},
			},
			targetConfig:   config.TargetConfig{},
			targetName:     "my-target",
			expectedName:   "my-target",
			expectedServer: "test.haloy.dev",
		},
		{
			name: "target name overrides base name",
			appConfig: config.AppConfig{
				TargetConfig: config.TargetConfig{
					Name: "base-name",
					Image: &config.Image{
						Repository: "nginx",
						Tag:        "latest",
					},
					Server: "test.haloy.dev",
				},
			},
			targetConfig: config.TargetConfig{
				Name: "target-override-name",
			},
			targetName:     "my-target",
			expectedName:   "target-override-name",
			expectedServer: "test.haloy.dev",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := MergeToTarget(tt.appConfig, tt.targetConfig, tt.targetName)
			if err != nil {
				t.Fatalf("MergeToTarget() unexpected error = %v", err)
			}

			if result.Name != tt.expectedName {
				t.Errorf("MergeToTarget() Name = %s, expected %s", result.Name, tt.expectedName)
			}

			if result.Server != tt.expectedServer {
				t.Errorf("MergeToTarget() Server = %s, expected %s", result.Server, tt.expectedServer)
			}

			if result.TargetName != tt.targetName {
				t.Errorf("MergeToTarget() TargetName = %s, expected %s", result.TargetName, tt.targetName)
			}

			if tt.expectedImage.Repository != "" {
				if result.Image.Repository != tt.expectedImage.Repository {
					t.Errorf("MergeToTarget() Image.Repository = %s, expected %s",
						result.Image.Repository, tt.expectedImage.Repository)
				}
				if result.Image.Tag != tt.expectedImage.Tag {
					t.Errorf("MergeToTarget() Image.Tag = %s, expected %s",
						result.Image.Tag, tt.expectedImage.Tag)
				}
				if tt.expectedImage.History != nil {
					if result.Image.History == nil {
						t.Errorf("MergeToTarget() Image.History should not be nil")
					} else {
						if result.Image.History.Strategy != tt.expectedImage.History.Strategy {
							t.Errorf("MergeToTarget() Image.History.Strategy = %s, expected %s",
								result.Image.History.Strategy, tt.expectedImage.History.Strategy)
						}
					}
				}
				if tt.expectedImage.RegistryAuth != nil {
					if result.Image.RegistryAuth == nil {
						t.Errorf("MergeToTarget() Image.RegistryAuth should not be nil")
					} else {
						if result.Image.RegistryAuth.Server != tt.expectedImage.RegistryAuth.Server {
							t.Errorf("MergeToTarget() Image.RegistryAuth.Server = %s, expected %s",
								result.Image.RegistryAuth.Server, tt.expectedImage.RegistryAuth.Server)
						}
					}
				}
			}

			// Test that normalization was applied
			if result.HealthCheckPath == "" {
				t.Errorf("MergeToTarget() HealthCheckPath should be normalized to default value")
			}
			if result.Port == "" {
				t.Errorf("MergeToTarget() Port should be normalized to default value")
			}
			if result.Replicas == nil {
				t.Errorf("MergeToTarget() Replicas should be normalized to default value")
			}
		})
	}
}

func TestMergeImage(t *testing.T) {
	baseImage := &config.Image{
		Repository: "nginx",
		Tag:        "1.20",
		History: &config.ImageHistory{
			Strategy: config.HistoryStrategyLocal,
			Count:    helpers.IntPtr(5),
		},
	}

	images := map[string]*config.Image{
		"web": {
			Repository: "apache",
			Tag:        "2.4",
		},
		"api": {
			Repository: "node",
			Tag:        "16",
		},
	}

	tests := []struct {
		name         string
		targetConfig config.TargetConfig
		images       map[string]*config.Image
		baseImage    *config.Image
		expected     *config.Image
		expectError  bool
		errMsg       string
	}{
		{
			name: "target image overrides base completely",
			targetConfig: config.TargetConfig{
				Image: &config.Image{
					Repository: "custom",
					Tag:        "latest",
				},
			},
			images:    images,
			baseImage: baseImage,
			expected: &config.Image{
				Repository: "custom",
				Tag:        "latest",
			},
		},
		{
			name: "target image merges with base",
			targetConfig: config.TargetConfig{
				Image: &config.Image{
					Tag: "1.21", // Only override tag
				},
			},
			images:    images,
			baseImage: baseImage,
			expected: &config.Image{
				Repository: "nginx", // From base
				Tag:        "1.21",  // Overridden
				History: &config.ImageHistory{
					Strategy: config.HistoryStrategyLocal,
					Count:    helpers.IntPtr(5),
				},
			},
		},
		{
			name: "imageKey resolves to images map",
			targetConfig: config.TargetConfig{
				ImageKey: "web",
			},
			images:    images,
			baseImage: baseImage,
			expected: &config.Image{
				Repository: "apache",
				Tag:        "2.4",
			},
		},
		{
			name: "imageKey not found in images map",
			targetConfig: config.TargetConfig{
				ImageKey: "nonexistent",
			},
			images:      images,
			baseImage:   baseImage,
			expectError: true,
			errMsg:      "imageRef 'nonexistent' not found in images map",
		},
		{
			name: "imageKey with nil images map",
			targetConfig: config.TargetConfig{
				ImageKey: "web",
			},
			images:      nil,
			baseImage:   baseImage,
			expectError: true,
			errMsg:      "imageRef 'web' specified but no images map defined",
		},
		{
			name:         "fallback to base image",
			targetConfig: config.TargetConfig{},
			images:       images,
			baseImage:    baseImage,
			expected:     baseImage,
		},
		{
			name:         "no image specified",
			targetConfig: config.TargetConfig{},
			images:       images,
			baseImage:    nil,
			expectError:  true,
			errMsg:       "no image specified for target",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := MergeImage(tt.targetConfig, tt.images, tt.baseImage)

			if tt.expectError {
				if err == nil {
					t.Errorf("MergeImage() expected error but got none")
				} else if tt.errMsg != "" && !helpers.Contains(err.Error(), tt.errMsg) {
					t.Errorf("MergeImage() error = %v, expected to contain %v", err, tt.errMsg)
				}
			} else {
				if err != nil {
					t.Errorf("MergeImage() unexpected error = %v", err)
				}
				if result == nil {
					t.Errorf("MergeImage() result should not be nil")
					return
				}
				if result.Repository != tt.expected.Repository {
					t.Errorf("MergeImage() Repository = %s, expected %s",
						result.Repository, tt.expected.Repository)
				}
				if result.Tag != tt.expected.Tag {
					t.Errorf("MergeImage() Tag = %s, expected %s",
						result.Tag, tt.expected.Tag)
				}
				if tt.expected.History != nil {
					if result.History == nil {
						t.Errorf("MergeImage() History should not be nil")
					} else if result.History.Strategy != tt.expected.History.Strategy {
						t.Errorf("MergeImage() History.Strategy = %s, expected %s",
							result.History.Strategy, tt.expected.History.Strategy)
					}
				}
			}
		})
	}
}

func TestExtractTargets(t *testing.T) {
	tests := []struct {
		name        string
		appConfig   config.AppConfig
		expectError bool
		errMsg      string
		expectCount int
	}{
		{
			name: "single target config",
			appConfig: config.AppConfig{
				TargetConfig: config.TargetConfig{
					Name: "myapp",
					Image: &config.Image{
						Repository: "nginx",
						Tag:        "latest",
					},
					Server: "test.haloy.dev",
				},
				Format: "yaml",
			},
			expectCount: 1,
		},
		{
			name: "multi target config",
			appConfig: config.AppConfig{
				TargetConfig: config.TargetConfig{
					Name: "myapp",
					Image: &config.Image{
						Repository: "nginx",
						Tag:        "latest",
					},
					Server: "default.haloy.dev",
				},
				Targets: map[string]*config.TargetConfig{
					"prod": {
						Server: "prod.haloy.dev",
					},
					"staging": {
						Server: "staging.haloy.dev",
					},
				},
				Format: "yaml",
			},
			expectCount: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ExtractTargets(tt.appConfig)
			if err != nil {
				t.Errorf("ExtractTargets() unexpected error = %v", err)
			}
			if len(result) != tt.expectCount {
				t.Errorf("ExtractTargets() result count = %d, expected %d", len(result), tt.expectCount)
			}
		})
	}
}
