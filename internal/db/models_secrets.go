package db

import (
	"crypto/md5"
	"database/sql"
	"encoding/hex"
	"fmt"
	"time"
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

// Upsert query: INSERT or UPDATE if name already exists
var upsertQuery = `
        INSERT INTO secrets (name, created_at, updated_at, encrypted_value)
        VALUES (?, ?, ?, ?)
        ON CONFLICT(name) DO UPDATE SET
            encrypted_value = excluded.encrypted_value,
            updated_at = excluded.updated_at
    `

// Upsert a secret, creating it if it doesn't exist or updating it if it does.
func (db *DB) SetSecret(name, encryptedValue string) error {
	if name == "" {
		return fmt.Errorf("secret name cannot be empty")
	}

	if encryptedValue == "" {
		return fmt.Errorf("secret value cannot be empty")
	}
	now := time.Now()

	_, err := db.Exec(upsertQuery, name, now, now, encryptedValue)
	if err != nil {
		return fmt.Errorf("failed to save secret: %w", err)
	}
	return nil
}

type SecretBatch struct {
	Name           string `json:"name"`
	EncryptedValue string `json:"encrypted_value"`
}

func (sb *SecretBatch) Validate() error {
	if sb.Name == "" {
		return fmt.Errorf("secret name cannot be empty")
	}
	if sb.EncryptedValue == "" {
		return fmt.Errorf("secret value cannot be empty")
	}
	return nil
}

// SetSecretsBatch inserts or updates multiple secrets in a single transaction.
func (db *DB) SetSecretsBatch(secrets []SecretBatch) error {
	if len(secrets) == 0 {
		return fmt.Errorf("no secrets provided")
	}

	for _, secret := range secrets {
		if err := secret.Validate(); err != nil {
			return fmt.Errorf("invalid secret %s: %w", secret.Name, err)
		}
	}

	tx := db.MustBegin()
	now := time.Now()

	for _, secret := range secrets {
		if err := secret.Validate(); err != nil {
			return fmt.Errorf("invalid secret %s: %w", secret.Name, err)
		}
		tx.MustExec(upsertQuery, secret.Name, now, now, secret.EncryptedValue)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

func (db *DB) GetSecretEncryptedValue(name string) (string, error) {
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

	return dbSecret.EncryptedValue, nil
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
