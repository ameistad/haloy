package config

import (
	"errors"
	"fmt"
	"reflect"

	"gopkg.in/yaml.v3"
)

// EnvVar represents an environment variable that can either have a plaintext value or be backed by a secret.
type EnvVar struct {
	Name string `yaml:"name"`

	// Use pointers to ensure only one is provided.
	Value      *string `yaml:"value,omitempty"`
	SecretName *string `yaml:"secretName,omitempty"`
	// Internal field to hold the decrypted value after processing.
	decryptedValue *string `yaml:"-"`
}

func (e *EnvVar) UnmarshalYAML(value *yaml.Node) error {
	// Get expected field names
	expectedFields := ExtractYAMLFieldNames(reflect.TypeOf(*e))

	// Check for unknown fields
	if err := CheckUnknownFields(value, expectedFields, "env var: "); err != nil {
		return err
	}

	// Use type alias to avoid infinite recursion
	type EnvVarAlias EnvVar
	var alias EnvVarAlias

	// Unmarshal to the alias type
	if err := value.Decode(&alias); err != nil {
		return err
	}

	// Copy data back to original struct
	*e = EnvVar(alias)

	return nil
}

// DecryptEnvVars iterates over the provided environment variables and, when a SecretName is set,
// looks up the corresponding encrypted secret, decrypts it using the age identity, and updates the variable.
func DecryptEnvVars(initialEnvVars []EnvVar) ([]EnvVar, error) {
	// If not secrets are provided, return the original env vars without initializing secrets.
	// We do this because the age ideetity might not be available in the current context and we can't load them.<
	hasSecrets := false
	for _, ev := range initialEnvVars {
		if ev.SecretName != nil {
			hasSecrets = true
			break
		}
	}
	if !hasSecrets {
		return initialEnvVars, nil
	}

	secrets, err := LoadSecrets()
	if err != nil {
		return nil, fmt.Errorf("failed to load secrets: %w", err)
	}

	// Load the full age identity (private key) — needed for decryption.
	identity, err := GetAgeIdentity()
	if err != nil {
		return nil, fmt.Errorf("failed to get age identity: %w", err)
	}

	envVars := make([]EnvVar, len(initialEnvVars))
	copy(envVars, initialEnvVars)
	for i, ev := range envVars {
		if ev.SecretName != nil {
			record, exists := secrets[*ev.SecretName]
			if !exists {
				continue
			}
			// DecryptSecret will use the full identity to decrypt the stored encrypted value.
			decrypted, err := DecryptSecret(record.Encrypted, identity)
			if err != nil {
				return nil, fmt.Errorf("failed to decrypt value for '%s': %w", ev.Name, err)
			}
			// Write back to the underlying slice element.
			envVars[i].decryptedValue = &decrypted
		}
	}
	return envVars, nil
}

// GetValue returns the final value of the environment variable. It returns the decrypted value if available;
// otherwise it returns the plaintext value. If neither is set, it returns an error.
func (ev *EnvVar) GetValue() (string, error) {
	if ev.decryptedValue != nil {
		return *ev.decryptedValue, nil
	}
	if ev.Value != nil {
		return *ev.Value, nil
	}
	// Failsafe: Should not happen if validation runs first.
	return "", fmt.Errorf("environment variable '%s' has neither a plaintext nor a decrypted value", ev.Name)
}

// Validate ensures the EnvVar is correctly configured.
func (ev *EnvVar) Validate() error {
	if ev.Name == "" {
		return errors.New("environment variable name cannot be empty")
	}
	if ev.Value != nil && ev.SecretName != nil {
		return fmt.Errorf("environment variable '%s': cannot provide both 'value' and 'secretName'", ev.Name)
	}
	if ev.Value == nil && ev.SecretName == nil {
		// Assuming that one must be provided...
		return fmt.Errorf("environment variable '%s': must provide either 'value' or 'secretName'", ev.Name)
	}

	if ev.SecretName != nil {
		secrets, err := LoadSecrets()
		if err != nil {
			return fmt.Errorf("failed to load secrets: %w", err)
		}
		if _, exists := secrets[*ev.SecretName]; !exists {
			return fmt.Errorf("secret '%s' not found in secrets store", *ev.SecretName)
		}
	}
	return nil
}
