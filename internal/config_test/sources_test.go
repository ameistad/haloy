package config_test

import (
	"testing"

	"github.com/ameistad/haloy/internal/config"
)

func TestSourceReference_Validate(t *testing.T) {
	tests := []struct {
		name    string
		ref     config.SourceReference
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid env reference",
			ref: config.SourceReference{
				Env: "DATABASE_URL",
			},
			wantErr: false,
		},
		{
			name: "valid secret reference",
			ref: config.SourceReference{
				Secret: "api-key",
			},
			wantErr: false,
		},
		{
			name:    "empty reference",
			ref:     config.SourceReference{},
			wantErr: true,
			errMsg:  "a source reference (e.g., 'env' or 'secret') must be specified",
		},
		{
			name: "both env and secret set",
			ref: config.SourceReference{
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
		vs      config.ValueSource
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid value source",
			vs: config.ValueSource{
				Value: "direct-value",
			},
			wantErr: false,
		},
		{
			name: "valid from reference",
			vs: config.ValueSource{
				From: &config.SourceReference{
					Env: "ENV_VAR",
				},
			},
			wantErr: false,
		},
		{
			name:    "empty value source",
			vs:      config.ValueSource{},
			wantErr: true,
			errMsg:  "must provide either 'value' or 'from'",
		},
		{
			name: "both value and from set",
			vs: config.ValueSource{
				Value: "direct-value",
				From: &config.SourceReference{
					Env: "ENV_VAR",
				},
			},
			wantErr: true,
			errMsg:  "cannot provide both 'value' and 'from'",
		},
		{
			name: "invalid from reference",
			vs: config.ValueSource{
				From: &config.SourceReference{
					Env:    "ENV_VAR",
					Secret: "secret-key",
				},
			},
			wantErr: true,
			errMsg:  "invalid 'from' block",
		},
		{
			name: "valid secret from reference",
			vs: config.ValueSource{
				From: &config.SourceReference{
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
