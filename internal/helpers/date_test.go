package helpers

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestFormatDateString(t *testing.T) {
	utc := time.UTC

	tests := []struct {
		name            string
		input           string
		expectedPattern string
		wantErr         bool
	}{
		{
			name:            "14_char_deployment_id",
			input:           "20250812150000",
			expectedPattern: `\d+ (second|minute|hour|day)s? (ago|from now)`,
			wantErr:         false,
		},
		{
			name:            "16_char_deployment_id",
			input:           "2025081215000050",
			expectedPattern: `\d+ (second|minute|hour|day)s? (ago|from now)`,
			wantErr:         false,
		},
		{
			name:            "rfc3339_format",
			input:           "2025-08-10T15:00:00Z",
			expectedPattern: `\d+ (second|minute|hour|day)s? (ago|from now)`,
			wantErr:         false,
		},
		{
			name:    "invalid_format",
			input:   "invalid-date",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := FormatDateStringWithLocation(tt.input, utc)

			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Regexp(t, tt.expectedPattern, result)
			}
		})
	}
}
