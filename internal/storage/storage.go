package storage

import (
	"database/sql"
	"fmt"
	"path/filepath"

	"github.com/ameistad/haloy/internal/config"
	"github.com/ameistad/haloy/internal/constants"
	_ "modernc.org/sqlite"
)

const driverName = "sqlite"

type DB struct {
	*sql.DB
}

func New() (*DB, error) {
	dataDir, err := config.DataDir()
	if err != nil {
		return nil, err
	}
	dbFile := filepath.Join(dataDir, constants.DBDir, constants.DBFileName)
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
	if _, err := database.Exec("PRAGMA busy_timeout = 5000"); err != nil {
		return nil, fmt.Errorf("failed to set busy timeout: %w", err)
	}
	if _, err := database.Exec("PRAGMA cache_size = 10000"); err != nil {
		return nil, fmt.Errorf("failed to set cache size: %w", err)
	}

	return &DB{database}, nil
}
