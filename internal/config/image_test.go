package config

import (
	"testing"
)

func TestImage_ImageRef(t *testing.T) {
	tests := []struct {
		name     string
		image    Image
		expected string
	}{
		{
			name: "repository with tag",
			image: Image{
				Repository: "nginx",
				Tag:        "1.21",
			},
			expected: "nginx:1.21",
		},
		{
			name: "repository without tag defaults to latest",
			image: Image{
				Repository: "nginx",
				Tag:        "",
			},
			expected: "nginx:latest",
		},
		{
			name: "repository with whitespace in tag",
			image: Image{
				Repository: "nginx",
				Tag:        " 1.21 ",
			},
			expected: "nginx:1.21",
		},
		{
			name: "repository with whitespace in repository",
			image: Image{
				Repository: " nginx ",
				Tag:        "1.21",
			},
			expected: "nginx:1.21",
		},
		{
			name: "full registry path",
			image: Image{
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
		image   Image
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid image with repository and tag",
			image: Image{
				Repository: "nginx",
				Tag:        "1.21",
			},
			wantErr: false,
		},
		{
			name: "valid image with registry source",
			image: Image{
				Repository: "nginx",
				Tag:        "1.21",
			},
			wantErr: false,
		},
		{
			name: "valid image with local source",
			image: Image{
				Repository: "myapp",
				Tag:        "latest",
			},
			wantErr: false,
		},
		{
			name: "empty repository",
			image: Image{
				Repository: "",
				Tag:        "1.21",
			},
			wantErr: true,
			errMsg:  "image.repository is required",
		},
		{
			name: "repository with whitespace",
			image: Image{
				Repository: "nginx latest",
				Tag:        "1.21",
			},
			wantErr: true,
			errMsg:  "contains whitespace",
		},
		{
			name: "tag with whitespace",
			image: Image{
				Repository: "nginx",
				Tag:        "1.21 2.0",
			},
			wantErr: true,
			errMsg:  "contains whitespace",
		},
		{
			name: "registry strategy with latest tag",
			image: Image{
				Repository: "nginx",
				Tag:        "latest",
				History: &ImageHistory{
					Strategy: HistoryStrategyRegistry,
					Count:    intPtr(5),
					Pattern:  "v*",
				},
			},
			wantErr: true,
			errMsg:  "cannot be 'latest' or empty with registry strategy",
		},
		{
			name: "registry strategy with mutable tag",
			image: Image{
				Repository: "myapp",
				Tag:        "main",
				History: &ImageHistory{
					Strategy: HistoryStrategyRegistry,
					Count:    intPtr(5),
					Pattern:  "v*",
				},
			},
			wantErr: true,
			errMsg:  "is mutable and not recommended",
		},
		{
			name: "valid registry strategy with immutable tag",
			image: Image{
				Repository: "myapp",
				Tag:        "v1.2.3",
				History: &ImageHistory{
					Strategy: HistoryStrategyRegistry,
					Count:    intPtr(5),
					Pattern:  "v*",
				},
			},
			wantErr: false,
		},
		{
			name: "valid registry auth",
			image: Image{
				Repository: "private.registry.com/myapp",
				Tag:        "v1.0.0",
				RegistryAuth: &RegistryAuth{
					Username: ValueSource{Value: "user"},
					Password: ValueSource{Value: "pass"},
				},
			},
			wantErr: false,
		},
		{
			name: "registry auth with whitespace in server",
			image: Image{
				Repository: "private.registry.com/myapp",
				Tag:        "v1.0.0",
				RegistryAuth: &RegistryAuth{
					Server:   "private registry.com",
					Username: ValueSource{Value: "user"},
					Password: ValueSource{Value: "pass"},
				},
			},
			wantErr: true,
			errMsg:  "contains whitespace",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.image.Validate("yaml")
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
		history ImageHistory
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid local strategy with count",
			history: ImageHistory{
				Strategy: HistoryStrategyLocal,
				Count:    intPtr(5),
			},
			wantErr: false,
		},
		{
			name: "valid registry strategy with count and pattern",
			history: ImageHistory{
				Strategy: HistoryStrategyRegistry,
				Count:    intPtr(10),
				Pattern:  "v*",
			},
			wantErr: false,
		},
		{
			name: "valid none strategy",
			history: ImageHistory{
				Strategy: HistoryStrategyNone,
			},
			wantErr: false,
		},
		{
			name: "empty strategy defaults to valid",
			history: ImageHistory{
				Strategy: "",
			},
			wantErr: false,
		},
		{
			name: "invalid strategy",
			history: ImageHistory{
				Strategy: "invalid-strategy",
			},
			wantErr: true,
			errMsg:  "must be 'local', 'registry', or 'none'",
		},
		{
			name: "local strategy missing count",
			history: ImageHistory{
				Strategy: HistoryStrategyLocal,
				Count:    nil,
			},
			wantErr: true,
			errMsg:  "count is required for local strategy",
		},
		{
			name: "registry strategy missing count",
			history: ImageHistory{
				Strategy: HistoryStrategyRegistry,
				Count:    nil,
			},
			wantErr: true,
			errMsg:  "count is required for registry strategy",
		},
		{
			name: "local strategy with zero count",
			history: ImageHistory{
				Strategy: HistoryStrategyLocal,
				Count:    intPtr(0),
			},
			wantErr: true,
			errMsg:  "must be at least 1",
		},
		{
			name: "registry strategy with negative count",
			history: ImageHistory{
				Strategy: HistoryStrategyRegistry,
				Count:    intPtr(-1),
			},
			wantErr: true,
			errMsg:  "must be at least 1",
		},
		{
			name: "registry strategy missing pattern",
			history: ImageHistory{
				Strategy: HistoryStrategyRegistry,
				Count:    intPtr(5),
				Pattern:  "",
			},
			wantErr: true,
			errMsg:  "pattern is required for registry strategy",
		},
		{
			name: "registry strategy with whitespace pattern",
			history: ImageHistory{
				Strategy: HistoryStrategyRegistry,
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
