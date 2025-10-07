package config_test

import (
	"testing"

	"github.com/ameistad/haloy/internal/config"
)

func TestEnvVar_Validate(t *testing.T) {
	tests := []struct {
		name    string
		envVar  config.EnvVar
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid env var with value",
			envVar: config.EnvVar{
				Name: "DATABASE_URL",
				ValueSource: config.ValueSource{
					Value: "postgres://localhost:5432/mydb",
				},
			},
			wantErr: false,
		},
		{
			name: "valid env var with from reference",
			envVar: config.EnvVar{
				Name: "API_KEY",
				ValueSource: config.ValueSource{
					From: &config.SourceReference{
						Env: "API_KEY_ENV",
					},
				},
			},
			wantErr: false,
		},
		{
			name: "empty env var name",
			envVar: config.EnvVar{
				Name: "",
				ValueSource: config.ValueSource{
					Value: "some-value",
				},
			},
			wantErr: true,
			errMsg:  "environment variable 'name' cannot be empty",
		},
		{
			name: "invalid value source - both value and from",
			envVar: config.EnvVar{
				Name: "INVALID_VAR",
				ValueSource: config.ValueSource{
					Value: "direct-value",
					From: &config.SourceReference{
						Env: "ENV_VAR",
					},
				},
			},
			wantErr: true,
			errMsg:  "cannot provide both 'value' and 'from'",
		},
		{
			name: "invalid value source - neither value nor from",
			envVar: config.EnvVar{
				Name:        "INVALID_VAR",
				ValueSource: config.ValueSource{},
			},
			wantErr: true,
			errMsg:  "must provide either 'value' or 'from'",
		},
		{
			name: "invalid from reference - both env and secret",
			envVar: config.EnvVar{
				Name: "INVALID_VAR",
				ValueSource: config.ValueSource{
					From: &config.SourceReference{
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
			envVar: config.EnvVar{
				Name: "INVALID_VAR",
				ValueSource: config.ValueSource{
					From: &config.SourceReference{},
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
