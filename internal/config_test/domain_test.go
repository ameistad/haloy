package config_test

import (
	"testing"

	"github.com/ameistad/haloy/internal/config"
)

func TestDomain_Validate(t *testing.T) {
	tests := []struct {
		name    string
		domain  config.Domain
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid domain with no aliases",
			domain: config.Domain{
				Canonical: "example.com",
				Aliases:   []string{},
			},
			wantErr: false,
		},
		{
			name: "valid domain with valid aliases",
			domain: config.Domain{
				Canonical: "example.com",
				Aliases:   []string{"www.example.com", "api.example.com"},
			},
			wantErr: false,
		},
		{
			name: "invalid canonical domain",
			domain: config.Domain{
				Canonical: "invalid domain",
				Aliases:   []string{},
			},
			wantErr: true,
			errMsg:  "domain must have at least two labels",
		},
		{
			name: "invalid alias domain",
			domain: config.Domain{
				Canonical: "example.com",
				Aliases:   []string{"valid.com", "invalid domain"},
			},
			wantErr: true,
			errMsg:  "alias 'invalid domain'",
		},
		{
			name: "empty canonical domain",
			domain: config.Domain{
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
