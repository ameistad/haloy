package db

func (db *DB) Migrate() error {
	schema := `
CREATE TABLE IF NOT EXISTS deployments (
    id TEXT PRIMARY KEY,                    -- Timestamp-based ID
    app_name TEXT NOT NULL,                 -- App being deployed
    app_config JSON NOT NULL,               -- Full AppConfig as JSON
    image_tag TEXT NOT NULL,                -- Docker image tag used
    rolled_back_from TEXT,                  -- ID of deployment this was rolled back from

    -- Foreign key constraint (optional)
    FOREIGN KEY (rolled_back_from) REFERENCES deployments(id)
);

-- Indexes for efficient queries
CREATE INDEX IF NOT EXISTS idx_deployments_app_name ON deployments(app_name);
CREATE INDEX IF NOT EXISTS idx_deployments_image_tag ON deployments(image_tag);
`

	_, err := db.Exec(schema)
	return err
}
