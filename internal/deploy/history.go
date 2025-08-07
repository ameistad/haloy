package deploy

import (
	"encoding/json"
	"fmt"

	"github.com/ameistad/haloy/internal/config"
	"github.com/ameistad/haloy/internal/constants"
	"github.com/ameistad/haloy/internal/storage"
)

// WriteAppConfigHistory writes the given appConfig to the history folder, naming the file <deploymentID>.yml.
func writeAppConfigHistory(appConfig config.AppConfig, deploymentID, imageRef string, deploymentsToKeep int) error {
	db, err := storage.New(constants.DBPath)
	if err != nil {
		return err
	}
	defer db.Close()
	appConfigJSON, err := json.Marshal(appConfig)
	if err != nil {
		return fmt.Errorf("failed to convert app config to JSON: %w", err)
	}
	deployment := storage.Deployment{
		ID:        deploymentID,
		AppName:   appConfig.Name,
		AppConfig: appConfigJSON,
		ImageRef:  imageRef,
	}

	if err := db.SaveDeployment(deployment); err != nil {
		return fmt.Errorf("failed to save deployment to database: %w", err)
	}

	// After writing, prune old deployment entries.
	if err := db.PruneOldDeployments(appConfig.Name, deploymentsToKeep); err != nil {
		return fmt.Errorf("failed to prune old deployments: %w", err)
	}

	return nil
}

// GetAppConfigHistory loads the AppConfig from the history file with the given deploymentID.
func GetAppConfigHistory(deploymentID string) (*config.AppConfig, error) {
	db, err := storage.New(constants.DBPath)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}
	defer db.Close()
	deployment, err := db.GetDeployment(deploymentID)
	if err != nil {
		return nil, fmt.Errorf("failed to get deployment from database: %w", err)
	}

	var appConfig config.AppConfig
	if err := json.Unmarshal(deployment.AppConfig, &appConfig); err != nil {
		return nil, fmt.Errorf("failed to unmarshal app config: %w", err)
	}

	return &appConfig, nil
}
