package db

import (
	"fmt"
	"path/filepath"

	"github.com/ameistad/haloy/internal/constants"
	"github.com/jmoiron/sqlx"

	_ "github.com/mattn/go-sqlite3"
)

const driverName = "sqlite3"
const dbName = "haloy.db"

type DB struct {
	*sqlx.DB
}

func New() (*DB, error) {
	dbPath := filepath.Join(constants.DBPath, dbName)
	database, err := sqlx.Connect(driverName, dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	// SQLite optimizations
	database.MustExec("PRAGMA foreign_keys = ON")
	database.MustExec("PRAGMA journal_mode = WAL")

	return &DB{database}, nil
}
