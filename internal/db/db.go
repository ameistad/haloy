package db

import (
	"fmt"
	"path/filepath"

	"github.com/ameistad/haloy/internal/config"
	"github.com/jmoiron/sqlx"
	_ "github.com/mattn/go-sqlite3"
)

type DB struct {
	*sqlx.DB
}

func New() (*DB, error) {
	dataDir, err := config.DataDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get data dir: %w", err)
	}

	dbPath := filepath.Join(dataDir, "haloy.db")
	database, err := sqlx.Connect("sqlite3", dbPath) // Change from "sqlite" to "sqlite3"
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	// SQLite optimizations
	database.MustExec("PRAGMA foreign_keys = ON")
	database.MustExec("PRAGMA journal_mode = WAL")

	return &DB{database}, nil
}
