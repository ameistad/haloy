package deploy

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"

	"github.com/ameistad/haloy/internal/config"
	"github.com/ameistad/haloy/internal/deploytypes"
	"github.com/ameistad/haloy/internal/docker"
	"github.com/ameistad/haloy/internal/storage"
	"github.com/docker/docker/client"
)

// RollbackApp is basically a wrapper around DeployApp that allows rolling back to a previous deployment.
func RollbackApp(ctx context.Context, cli *client.Client, appName, targetDeploymentID, newDeploymentID string, logger *slog.Logger) error {
	targets, err := GetRollbackTargets(ctx, cli, appName)
	if err != nil {
		return err
	}

	if len(targets) == 0 {
		return fmt.Errorf("there are no images to rollback to for %s", appName)
	}

	for _, t := range targets {
		if t.DeploymentID == targetDeploymentID {
			if t.AppConfig == nil {
				return fmt.Errorf("failed to load app config for %s: %w", appName, err)
			}
			// Adding config format here doesn't seem necessary as it is mainly used to return better validation errors.
			// If we already have used the config it's already been validated.
			if err := DeployApp(ctx, cli, newDeploymentID, *t.AppConfig, "", logger); err != nil {
				return fmt.Errorf("failed to deploy app %s: %w", appName, err)
			}

			// found the target and deployment successfull
			return nil
		}
	}

	return fmt.Errorf("deployment ID '%s' not found for app '%s'", targetDeploymentID, appName)
}

// GetRollbackTargets retrieves and sorts all available rollback targets for the specified app.
func GetRollbackTargets(ctx context.Context, cli *client.Client, appName string) (targets []deploytypes.RollbackTarget, err error) {
	if appName == "" {
		return targets, fmt.Errorf("app name cannot be empty")
	}

	db, err := storage.New()
	if err != nil {
		return targets, fmt.Errorf("failed to connect to database: %w", err)
	}
	defer db.Close()

	deployments, err := db.GetDeploymentHistory(appName, 50)
	if err != nil {
		return targets, fmt.Errorf("failed to get deployment history: %w", err)
	}

	runningDeploymentID, _ := getRunningDeploymentID(ctx, cli, appName)

	for _, deployment := range deployments {
		// Parse deployed image config
		deployedImage, err := deployment.GetRollbackImageConfig()
		if err != nil {
			continue // Skip malformed image configs
		}

		// Skip deployments with "none" strategy
		if deployedImage.History != nil && deployedImage.History.Strategy == config.HistoryStrategyNone {
			continue
		}

		// Get image reference
		imageRef, err := deployment.GetImageRef()
		if err != nil {
			continue
		}

		// Check if image is available based on strategy
		strategy := config.HistoryStrategyLocal
		if deployedImage.History != nil {
			strategy = deployedImage.History.Strategy
		}

		available, err := isImageAvailable(ctx, cli, imageRef, strategy)
		if err != nil || !available {
			continue
		}

		// Parse original config and replace the image with the deployed one
		var rollbackConfig config.AppConfig
		if err := json.Unmarshal(deployment.AppConfig, &rollbackConfig); err != nil {
			continue
		}

		// Replace the image in the config with the deployed image
		rollbackConfig.Image = deployedImage

		target := deploytypes.RollbackTarget{
			DeploymentID: deployment.ID,
			ImageRef:     imageRef,
			IsRunning:    deployment.ID == runningDeploymentID,
			AppConfig:    &rollbackConfig,
		}

		targets = append(targets, target)
	}

	return targets, nil
}

func isImageAvailable(ctx context.Context, cli *client.Client, imageRef string, strategy config.HistoryStrategy) (bool, error) {
	switch strategy {
	case config.HistoryStrategyLocal:
		_, err := cli.ImageInspect(ctx, imageRef)
		return err == nil, nil

	case config.HistoryStrategyRegistry:
		return true, nil // Assume registry images are available

	case config.HistoryStrategyNone:
		return false, nil

	default:
		return false, fmt.Errorf("unknown strategy: %s", strategy)
	}
}

func getRunningDeploymentID(ctx context.Context, cli *client.Client, appName string) (string, error) {
	ContainerList, err := docker.GetAppContainers(ctx, cli, false, appName)
	if err != nil {
		return "", err
	}

	if len(ContainerList) == 0 {
		return "", fmt.Errorf("no running containers found for app %s", appName)
	}

	deploymentIDs := make([]string, 0, len(ContainerList))
	for _, container := range ContainerList {
		id := container.Labels[config.LabelDeploymentID]
		if id != "" {
			deploymentIDs = append(deploymentIDs, id)
		}
	}
	if len(deploymentIDs) == 0 {
		return "", fmt.Errorf("no deployment IDs found in running containers for app %s", appName)
	}

	sort.Slice(deploymentIDs, func(i, j int) bool {
		return deploymentIDs[i] > deploymentIDs[j]
	})

	return deploymentIDs[0], nil
}
