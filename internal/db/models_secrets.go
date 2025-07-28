package db

import (
	"bytes"
	"crypto/md5"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"time"

	"filippo.io/age"
	"github.com/ameistad/haloy/internal/constants"
)

type Secret struct {
	Name           string    `db:"name" json:"name"`
	EncryptedValue string    `db:"encrypted_value" json:"encrypted_value"`
	CreatedAt      time.Time `db:"created_at" json:"created_at"`
	UpdatedAt      time.Time `db:"updated_at" json:"updated_at"`
}

type SecretAPIResponse struct {
	Name        string `json:"name"`
	DigestValue string `json:"digest_value"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

func (s Secret) ToAPIResponse() SecretAPIResponse {
	digest := md5.Sum([]byte(s.EncryptedValue))
	digestStr := hex.EncodeToString(digest[:])
	return SecretAPIResponse{
		Name:        s.Name,
		DigestValue: digestStr,
		CreatedAt:   s.CreatedAt.Format(time.RFC3339),
		UpdatedAt:   s.UpdatedAt.Format(time.RFC3339),
	}
}

func createSecretsTable(db *DB) error {
	schema := `
CREATE TABLE IF NOT EXISTS secrets (
    name TEXT PRIMARY KEY,                  -- User-defined secret name
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    encrypted_value TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_secrets_created_at ON secrets(created_at);
CREATE INDEX IF NOT EXISTS idx_secrets_updated_at ON secrets(updated_at);
`

	_, err := db.Exec(schema)
	if err != nil {
		return fmt.Errorf("failed to create secrets table: %w", err)
	}
	return nil
}

// Will upsert a secret, creating it if it doesn't exist or updating it if it does.
func (db *DB) SetSecret(name, value string) error {
	if name == "" {
		return fmt.Errorf("secret name cannot be empty")
	}

	if value == "" {
		return fmt.Errorf("secret value cannot be empty")
	}
	identity, err := getAgeIdentity()
	if err != nil {
		return fmt.Errorf("failed to get encryption key: %w", err)
	}
	encryptedValue, err := encryptSecret(value, identity.Recipient())
	if err != nil {
		return fmt.Errorf("failed to encrypt secret value: %w", err)
	}
	if encryptedValue == "" {
		return fmt.Errorf("encrypted secret value is empty")
	}
	now := time.Now()

	// Upsert query: INSERT or UPDATE if name already exists
	query := `
        INSERT INTO secrets (name, created_at, updated_at, encrypted_value)
        VALUES (?, ?, ?, ?)
        ON CONFLICT(name) DO UPDATE SET
            encrypted_value = excluded.encrypted_value,
            updated_at = excluded.updated_at
    `

	_, err = db.Exec(query, name, now, now, encryptedValue)
	if err != nil {
		return fmt.Errorf("failed to save secret: %w", err)
	}
	return nil
}

func (db *DB) GetSecretDecryptedValue(name string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("secret name cannot be empty")
	}

	var dbSecret Secret
	query := `SELECT name, created_at, updated_at, encrypted_value FROM secrets WHERE name = ?`

	err := db.Get(&dbSecret, query, name)
	if err != nil {
		if err == sql.ErrNoRows {
			return "", fmt.Errorf("secret '%s' not found", name)
		}
		return "", fmt.Errorf("failed to get secret: %w", err)
	}

	// Get age identity for decryption
	identity, err := getAgeIdentity()
	if err != nil {
		return "", fmt.Errorf("failed to get encryption key: %w", err)
	}

	decryptedValue, err := decryptSecret(dbSecret.EncryptedValue, identity)
	if err != nil {
		return "", fmt.Errorf("failed to decrypt secret '%s': %w", name, err)
	}

	return decryptedValue, nil
}

func (db *DB) GetSecretsList() ([]Secret, error) {
	var secrets []Secret
	query := `SELECT name, created_at, updated_at, encrypted_value FROM secrets ORDER BY updated_at DESC`

	err := db.Select(&secrets, query)
	if err != nil {
		return nil, fmt.Errorf("failed to list secrets: %w", err)
	}

	return secrets, nil
}

func (db *DB) DeleteSecret(name string) error {
	query := `DELETE FROM secrets WHERE name = ?`

	result, err := db.Exec(query, name)
	if err != nil {
		return fmt.Errorf("failed to delete secret: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to check rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("secret '%s' not found", name)
	}

	return nil
}

func (db *DB) SecretExists(name string) (bool, error) {
	if name == "" {
		return false, fmt.Errorf("secret name cannot be empty")
	}

	var exists bool
	query := `SELECT EXISTS(SELECT 1 FROM secrets WHERE name = ?)`

	err := db.Get(&exists, query, name)
	if err != nil {
		return false, fmt.Errorf("failed to check if secret exists: %w", err)
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
