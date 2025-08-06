package db

import (
	"database/sql"
	"fmt"
	"path/filepath"

	_ "modernc.org/sqlite"
)

const driverName = "sqlite"
const dbName = "haloy.db"

type DB struct {
	*sql.DB
}

func New(dbPath string) (*DB, error) {
	dbFile := filepath.Join(dbPath, dbName)
	database, err := sql.Open(driverName, dbFile)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	if err := database.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	if _, err := database.Exec("PRAGMA foreign_keys = ON"); err != nil {
		return nil, fmt.Errorf("failed to enable foreign keys: %w", err)
	}
	if _, err := database.Exec("PRAGMA journal_mode = WAL"); err != nil {
		return nil, fmt.Errorf("failed to set journal mode: %w", err)
	}

	return &DB{database}, nil
}
