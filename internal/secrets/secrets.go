package secrets

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"os"

	"filippo.io/age"
	"github.com/ameistad/haloy/internal/constants"
	"github.com/ameistad/haloy/internal/db"
)

type Manager struct {
	db *db.DB
}

func NewManager() (*Manager, error) {
	db, err := db.New()
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err) // ← Return the error!
	}
	return &Manager{db: db}, nil
}

func (s *Manager) SetSecret(name, value string) error {
	if name == "" {
		return fmt.Errorf("secret name cannot be empty")
	}

	if value == "" {
		return fmt.Errorf("secret value cannot be empty")
	}

	// Get age recipient for encryption
	identity, err := getAgeIdentity()
	if err != nil {
		return fmt.Errorf("failed to get encryption key: %w", err)
	}

	// Encrypt the value
	encryptedValue, err := encryptSecret(value, identity.Recipient())
	if err != nil {
		return fmt.Errorf("failed to encrypt secret: %w", err)
	}

	dbSecret := db.Secret{
		Name:  name,
		Value: encryptedValue,
	}

	if err := s.db.UpsertSecret(dbSecret); err != nil {
		return err
	}

	return nil
}

func (s *Manager) GetDecryptedValue(name string) (string, error) {
	// Get encrypted secret from database
	dbSecret, err := s.db.GetSecret(name)
	if err != nil {
		return "", err
	}

	// Get age identity for decryption
	identity, err := getAgeIdentity()
	if err != nil {
		return "", fmt.Errorf("failed to get encryption key: %w", err)
	}

	// Decrypt the value
	decryptedValue, err := decryptSecret(dbSecret.Value, identity)
	if err != nil {
		return "", fmt.Errorf("failed to decrypt secret '%s': %w", name, err)
	}

	return decryptedValue, nil
}

func (m *Manager) SecretExists(name string) (bool, error) {
	exists, err := m.db.SecretExists(name)
	if err != nil {
		return false, err
	}
	return exists, nil
}

func getAgeIdentity() (*age.X25519Identity, error) {
	identityStr := os.Getenv(constants.EnvVarAgeIdentity)
	if identityStr == "" {
		return nil, fmt.Errorf("environment variable %s is not set", constants.EnvVarAgeIdentity)
	}
	identity, err := age.ParseX25519Identity(identityStr)
	if err != nil {
		return nil, fmt.Errorf("failed to parse age identity from %s environment variable: %w", constants.EnvVarAgeIdentity, err)
	}
	return identity, nil
}

// EncryptSecret encrypts a plain-text value using the provided age recipient.
// It returns the encrypted value as a base64-encoded string for storage.
func encryptSecret(value string, recipient age.Recipient) (string, error) {
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
func decryptSecret(secret string, identity age.Identity) (string, error) {
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
