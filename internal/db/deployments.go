package db

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
