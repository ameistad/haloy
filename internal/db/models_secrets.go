package db

import (
	"database/sql"
	"fmt"
	"time"
)

type Secret struct {
	Name      string    `db:"name" json:"name"`
	Value     string    `db:"value" json:"value"`
	CreatedAt time.Time `db:"created_at" json:"created_at"`
	UpdatedAt time.Time `db:"updated_at" json:"updated_at"`
}

func (s Secret) CreateTable(db *DB) error {
	schema := `
CREATE TABLE IF NOT EXISTS secrets (
    name TEXT PRIMARY KEY,                  -- User-defined secret name
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    value TEXT NOT NULL
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

func (db *DB) UpsertSecret(secret Secret) error {
	if secret.Name == "" {
		return fmt.Errorf("secret name cannot be empty")
	}

	now := time.Now()
	if secret.CreatedAt.IsZero() {
		secret.CreatedAt = now
	}
	secret.UpdatedAt = now

	// Upsert query: INSERT or UPDATE if name already exists
	query := `
        INSERT INTO secrets (name, created_at, updated_at, value)
        VALUES (:name, :created_at, :updated_at, :value)
        ON CONFLICT(name) DO UPDATE SET
            value = excluded.value,
            updated_at = excluded.updated_at
        -- Note: we don't update created_at on conflict, keep the original
    `

	_, err := db.NamedExec(query, secret)
	if err != nil {
		return fmt.Errorf("failed to save secret: %w", err)
	}
	return nil
}

func (db *DB) GetSecret(name string) (*Secret, error) {
	if name == "" {
		return nil, fmt.Errorf("secret name cannot be empty")
	}

	var secret Secret
	query := `SELECT name, created_at, updated_at, value FROM secrets WHERE name = ?`

	err := db.Get(&secret, query, name)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("secret '%s' not found", name)
		}
		return nil, fmt.Errorf("failed to get secret: %w", err)
	}

	return &secret, nil
}

func (db *DB) ListSecrets() ([]Secret, error) {
	var secrets []Secret
	query := `SELECT name, created_at, updated_at, value FROM secrets ORDER BY updated_at DESC`

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

// func isUniqueConstraintError(err error) bool {
// 	// SQLite unique constraint error
// 	return strings.Contains(err.Error(), "UNIQUE constraint failed")
// }
