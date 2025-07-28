package db

import (
	"encoding/json"
	"fmt"
)

type Deployment struct {
	ID             string          `db:"id" json:"id"`
	AppName        string          `db:"app_name" json:"app_name"`
	AppConfig      json.RawMessage `db:"app_config" json:"app_config"`
	ImageTag       string          `db:"image_tag" json:"image_tag"`
	RolledBackFrom *string         `db:"rolled_back_from" json:"rolled_back_from,omitempty"`
}

func createDeploymentsTable(db *DB) error {
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
	if err != nil {
		return fmt.Errorf("failed to create deployments table: %w", err)
	}
	return nil
}

func (db *DB) SaveDeployment(deployment Deployment) error {
	query := `INSERT INTO deployments (id, app_name, app_config, image_tag, rolled_back_from)
              VALUES (?, ?, ?, ?, ?)`
	_, err := db.Exec(query, deployment.ID, deployment.AppName, deployment.AppConfig,
		deployment.ImageTag, deployment.RolledBackFrom)
	return err
}

func (db *DB) GetDeployment(deploymentID string) (Deployment, error) {
	var deployment Deployment
	query := `SELECT id, app_name, app_config, image_tag, rolled_back_from
              FROM deployments WHERE id = ?`
	err := db.Get(&deployment, query, deploymentID)
	return deployment, err
}

func (db *DB) GetDeploymentHistory(appName string, limit int) ([]Deployment, error) {
	var deployments []Deployment
	query := `SELECT id, app_name, app_config, image_tag, rolled_back_from
              FROM deployments
              WHERE app_name = ?
              ORDER BY id DESC
              LIMIT ?`
	err := db.Select(&deployments, query, appName, limit)
	return deployments, err
}

func (db *DB) GetDeploymentsByImageTag(imageTag string) ([]Deployment, error) {
	var deployments []Deployment
	query := `SELECT id, app_name, app_config, image_tag, rolled_back_from
              FROM deployments
              WHERE image_tag = ?
              ORDER BY id DESC`
	err := db.Select(&deployments, query, imageTag)
	return deployments, err
}

func (db *DB) PruneOldDeployments(appName string, deploymentsToKeep int) error {
	// Keep the N most recent deployments for this app, delete the rest
	// Since ID is in YYYYMMDDHHMMSS format, we can sort by ID directly
	query := `
        DELETE FROM deployments
        WHERE app_name = ?
        AND id NOT IN (
            SELECT id FROM deployments
            WHERE app_name = ?
            ORDER BY id DESC
            LIMIT ?
        )
    `

	result, err := db.Exec(query, appName, appName, deploymentsToKeep)
	if err != nil {
		return fmt.Errorf("failed to prune old deployments: %w", err)
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected > 0 {
		fmt.Printf("Pruned %d old deployment(s) for app '%s'\n", rowsAffected, appName)
	}

	return nil
}
