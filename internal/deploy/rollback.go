package deploy

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"

	"github.com/ameistad/haloy/internal/config"
	"github.com/ameistad/haloy/internal/deploytypes"
	"github.com/ameistad/haloy/internal/docker"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
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

	// Get available images for the app
	// List all images for the app that match the format appName:<deploymentID>.
	images, err := cli.ImageList(ctx, image.ListOptions{
		Filters: filters.NewArgs(filters.Arg("reference", appName+":*")),
	})
	if err != nil {
		return targets, fmt.Errorf("failed to list images for %s: %w", appName, err)
	}

	runningDeploymentID, _ := getRunningDeploymentID(ctx, cli, appName)

	for _, img := range images {
		for _, imageRef := range img.RepoTags {
			if strings.HasSuffix(imageRef, ":latest") {
				continue
			}
			// Expected tag format: "appName:deploymentID", e.g. "test-app:20250615214304"
			parts := strings.SplitN(imageRef, ":", 2)
			if len(parts) != 2 {
				// Unexpected tag format, skip this tag.
				continue
			}
			deploymentID := parts[1]
			appConfig, _ := GetAppConfigHistory(deploymentID)
			target := deploytypes.RollbackTarget{
				DeploymentID: deploymentID,
				ImageID:      img.ID,
				ImageRef:     imageRef,
				IsRunning:    deploymentID == runningDeploymentID,
				AppConfig:    appConfig,
			}

			targets = append(targets, target)
		}
	}

	sort.Slice(targets, func(i, j int) bool {
		return targets[i].DeploymentID > targets[j].DeploymentID // Newest first
	})

	return targets, nil
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
