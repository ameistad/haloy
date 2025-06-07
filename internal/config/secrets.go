package config

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"filippo.io/age"
)

const (
	IdentityFileName = "age_identity.txt"
	SecretsFileName  = "secrets.json"
)

type SecretRecord struct {
	Encrypted string `json:"encrypted"`
	Date      string `json:"date"`
}

// GetAgeRecipient reads the age identity file and returns the corresponding recipient.
func GetAgeRecipient() (age.Recipient, error) {
	configDir, err := ConfigDirPath()
	if err != nil {
		return nil, fmt.Errorf("failed to get config directory: %w", err)
	}
	identityPath := filepath.Join(configDir, IdentityFileName)
	data, err := os.ReadFile(identityPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("age identity file not found at %s - please run 'secrets init' first", identityPath)
		}
		return nil, fmt.Errorf("failed to read age identity file: %w", err)
	}
	identityStr := strings.TrimSpace(string(data))
	identity, err := age.ParseX25519Identity(identityStr)
	if err != nil {
		return nil, fmt.Errorf("failed to parse age identity from file: %w", err)
	}
	return identity.Recipient(), nil
}

func GetAgeIdentity() (age.Identity, error) {
	configDir, err := ConfigDirPath()
	if err != nil {
		return nil, fmt.Errorf("failed to get config directory: %w", err)
	}
	identityPath := filepath.Join(configDir, IdentityFileName)
	data, err := os.ReadFile(identityPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("age identity file not found at %s - please run 'secrets init' first", identityPath)
		}
		return nil, fmt.Errorf("failed to read age identity file: %w", err)
	}
	identityStr := strings.TrimSpace(string(data))
	identity, err := age.ParseX25519Identity(identityStr)
	if err != nil {
		return nil, fmt.Errorf("failed to parse age identity from file: %w", err)
	}
	return identity, nil
}

// LoadSecrets loads the secrets map from secrets.json (or returns an empty map if not found).
func LoadSecrets() (map[string]SecretRecord, error) {
	configDir, err := ConfigDirPath()
	if err != nil {
		return nil, err
	}
	secretsPath := filepath.Join(configDir, SecretsFileName)
	secrets := make(map[string]SecretRecord)
	data, err := os.ReadFile(secretsPath)
	if err != nil {
		if os.IsNotExist(err) {
			return secrets, nil
		}
		return nil, fmt.Errorf("failed to read secrets file: %w", err)
	}
	if err := json.Unmarshal(data, &secrets); err != nil {
		return nil, fmt.Errorf("failed to parse secrets file: %w", err)
	}
	return secrets, nil
}

// EncryptSecret encrypts a plain-text value using the provided age recipient.
// It returns the encrypted value as a base64-encoded string for storage.
func EncryptSecret(value string, recipient age.Recipient) (string, error) {
	var rawBuffer bytes.Buffer
	encryptWriter, err := age.Encrypt(&rawBuffer, recipient)
	if err != nil {
		return "", fmt.Errorf("failed to initialize encryptor: %w", err)
	}
	if _, err = io.WriteString(encryptWriter, value); err != nil {
		return "", fmt.Errorf("failed to write value to encryption writer: %w", err)
	}
	if err := encryptWriter.Close(); err != nil {
		return "", fmt.Errorf("failed to close encryption writer: %w", err)
	}
	encodedValue := base64.StdEncoding.EncodeToString(rawBuffer.Bytes())
	return encodedValue, nil
}

// DecryptSecret decrypts a base64-encoded secret using the provided age identity.
// It returns the decrypted secret as a string.
func DecryptSecret(secret string, identity age.Identity) (string, error) {
	// Decode the stored encrypted value from its base64 representation.
	encryptedBytes, err := base64.StdEncoding.DecodeString(secret)
	if err != nil {
		return "", fmt.Errorf("failed to decode base64 secret: %w", err)
	}

	decryptReader, err := age.Decrypt(bytes.NewReader(encryptedBytes), identity)
	if err != nil {
		return "", fmt.Errorf("failed to decrypt value: %w", err)
	}

	var decryptedBuf bytes.Buffer
	if _, err := io.Copy(&decryptedBuf, decryptReader); err != nil {
		return "", fmt.Errorf("failed to read decrypted value: %w", err)
	}

	return decryptedBuf.String(), nil
}

// SaveSecrets writes the secrets map to secrets.json.
func SaveSecrets(secrets map[string]SecretRecord) error {
	configDir, err := ConfigDirPath()
	if err != nil {
		return err
	}
	secretsPath := filepath.Join(configDir, SecretsFileName)
	data, err := json.MarshalIndent(secrets, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to encode secrets as JSON: %w", err)
	}
	// Write with restricted permissions (0600 - read/write for owner only)
	if err := os.WriteFile(secretsPath, data, 0600); err != nil {
		return fmt.Errorf("failed to write secrets file: %w", err)
	}
	return nil
}
