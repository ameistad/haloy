package config_test

import (
	"testing"

	"github.com/ameistad/haloy/internal/config"
)

func TestImage_ImageRef(t *testing.T) {
	tests := []struct {
		name     string
		image    config.Image
		expected string
	}{
		{
			name: "repository with tag",
			image: config.Image{
				Repository: "nginx",
				Tag:        "1.21",
			},
			expected: "nginx:1.21",
		},
		{
			name: "repository without tag defaults to latest",
			image: config.Image{
				Repository: "nginx",
				Tag:        "",
			},
			expected: "nginx:latest",
		},
		{
			name: "repository with whitespace in tag",
			image: config.Image{
				Repository: "nginx",
				Tag:        " 1.21 ",
			},
			expected: "nginx:1.21",
		},
		{
			name: "repository with whitespace in repository",
			image: config.Image{
				Repository: " nginx ",
				Tag:        "1.21",
			},
			expected: "nginx:1.21",
		},
		{
			name: "full registry path",
			image: config.Image{
				Repository: "registry.example.com/myapp",
				Tag:        "v1.0.0",
			},
			expected: "registry.example.com/myapp:v1.0.0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.image.ImageRef()
			if result != tt.expected {
				t.Errorf("ImageRef() = %s, expected %s", result, tt.expected)
			}
		})
	}
}

func TestImage_Validate(t *testing.T) {
	tests := []struct {
		name    string
		image   config.Image
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid image with repository and tag",
			image: config.Image{
				Repository: "nginx",
				Tag:        "1.21",
			},
			wantErr: false,
		},
		{
			name: "valid image with registry source",
			image: config.Image{
				Repository: "nginx",
				Tag:        "1.21",
				Source:     config.ImageSourceRegistry,
			},
			wantErr: false,
		},
		{
			name: "valid image with local source",
			image: config.Image{
				Repository: "myapp",
				Tag:        "latest",
				Source:     config.ImageSourceLocal,
			},
			wantErr: false,
		},
		{
			name: "empty repository",
			image: config.Image{
				Repository: "",
				Tag:        "1.21",
			},
			wantErr: true,
			errMsg:  "image.repository is required",
		},
		{
			name: "repository with whitespace",
			image: config.Image{
				Repository: "nginx latest",
				Tag:        "1.21",
			},
			wantErr: true,
			errMsg:  "contains whitespace",
		},
		{
			name: "invalid source",
			image: config.Image{
				Repository: "nginx",
				Tag:        "1.21",
				Source:     "invalid-source",
			},
			wantErr: true,
			errMsg:  "must be 'registry' or 'local'",
		},
		{
			name: "tag with whitespace",
			image: config.Image{
				Repository: "nginx",
				Tag:        "1.21 2.0",
			},
			wantErr: true,
			errMsg:  "contains whitespace",
		},
		{
			name: "registry strategy with latest tag",
			image: config.Image{
				Repository: "nginx",
				Tag:        "latest",
				History: &config.ImageHistory{
					Strategy: config.HistoryStrategyRegistry,
					Count:    intPtr(5),
					Pattern:  "v*",
				},
			},
			wantErr: true,
			errMsg:  "cannot be 'latest' or empty with registry strategy",
		},
		{
			name: "registry strategy with mutable tag",
			image: config.Image{
				Repository: "myapp",
				Tag:        "main",
				History: &config.ImageHistory{
					Strategy: config.HistoryStrategyRegistry,
					Count:    intPtr(5),
					Pattern:  "v*",
				},
			},
			wantErr: true,
			errMsg:  "is mutable and not recommended",
		},
		{
			name: "valid registry strategy with immutable tag",
			image: config.Image{
				Repository: "myapp",
				Tag:        "v1.2.3",
				History: &config.ImageHistory{
					Strategy: config.HistoryStrategyRegistry,
					Count:    intPtr(5),
					Pattern:  "v*",
				},
			},
			wantErr: false,
		},
		{
			name: "valid registry auth",
			image: config.Image{
				Repository: "private.registry.com/myapp",
				Tag:        "v1.0.0",
				RegistryAuth: &config.RegistryAuth{
					Username: config.ValueSource{Value: "user"},
					Password: config.ValueSource{Value: "pass"},
				},
			},
			wantErr: false,
		},
		{
			name: "registry auth with whitespace in server",
			image: config.Image{
				Repository: "private.registry.com/myapp",
				Tag:        "v1.0.0",
				RegistryAuth: &config.RegistryAuth{
					Server:   "private registry.com",
					Username: config.ValueSource{Value: "user"},
					Password: config.ValueSource{Value: "pass"},
				},
			},
			wantErr: true,
			errMsg:  "contains whitespace",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.image.Validate()
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

func TestImageHistory_Validate(t *testing.T) {
	tests := []struct {
		name    string
		history config.ImageHistory
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid local strategy with count",
			history: config.ImageHistory{
				Strategy: config.HistoryStrategyLocal,
				Count:    intPtr(5),
			},
			wantErr: false,
		},
		{
			name: "valid registry strategy with count and pattern",
			history: config.ImageHistory{
				Strategy: config.HistoryStrategyRegistry,
				Count:    intPtr(10),
				Pattern:  "v*",
			},
			wantErr: false,
		},
		{
			name: "valid none strategy",
			history: config.ImageHistory{
				Strategy: config.HistoryStrategyNone,
			},
			wantErr: false,
		},
		{
			name: "empty strategy defaults to valid",
			history: config.ImageHistory{
				Strategy: "",
			},
			wantErr: false,
		},
		{
			name: "invalid strategy",
			history: config.ImageHistory{
				Strategy: "invalid-strategy",
			},
			wantErr: true,
			errMsg:  "must be 'local', 'registry', or 'none'",
		},
		{
			name: "local strategy missing count",
			history: config.ImageHistory{
				Strategy: config.HistoryStrategyLocal,
				Count:    nil,
			},
			wantErr: true,
			errMsg:  "count is required for local strategy",
		},
		{
			name: "registry strategy missing count",
			history: config.ImageHistory{
				Strategy: config.HistoryStrategyRegistry,
				Count:    nil,
			},
			wantErr: true,
			errMsg:  "count is required for registry strategy",
		},
		{
			name: "local strategy with zero count",
			history: config.ImageHistory{
				Strategy: config.HistoryStrategyLocal,
				Count:    intPtr(0),
			},
			wantErr: true,
			errMsg:  "must be at least 1",
		},
		{
			name: "registry strategy with negative count",
			history: config.ImageHistory{
				Strategy: config.HistoryStrategyRegistry,
				Count:    intPtr(-1),
			},
			wantErr: true,
			errMsg:  "must be at least 1",
		},
		{
			name: "registry strategy missing pattern",
			history: config.ImageHistory{
				Strategy: config.HistoryStrategyRegistry,
				Count:    intPtr(5),
				Pattern:  "",
			},
			wantErr: true,
			errMsg:  "pattern is required for registry strategy",
		},
		{
			name: "registry strategy with whitespace pattern",
			history: config.ImageHistory{
				Strategy: config.HistoryStrategyRegistry,
				Count:    intPtr(5),
				Pattern:  "   ",
			},
			wantErr: true,
			errMsg:  "pattern is required for registry strategy",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.history.Validate()
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
