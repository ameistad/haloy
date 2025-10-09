package config

import (
	"reflect"
	"testing"
)

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
