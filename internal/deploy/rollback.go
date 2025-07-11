package deploy

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/ameistad/haloy/internal/config"
	"github.com/ameistad/haloy/internal/docker"
	"github.com/ameistad/haloy/internal/ui"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/client"
)

type RollbackTarget struct {
	DeploymentID string
	ImageID      string
	ImageTag     string
	IsLatest     bool
	AppConfig    *config.AppConfig
}

func RollbackApp(ctx context.Context, cli *client.Client, appName, targetDeploymentID string) error {
	targets, err := GetRollbackTargets(ctx, cli, appName)
	if err != nil {
		return err
	}

	if len(targets) == 0 {
		return fmt.Errorf("there are no images to rollback to for %s", appName)
	}

	for _, t := range targets {
		if t.DeploymentID == targetDeploymentID {
			newDeploymentID := createDeploymentID()
			newImageTag, err := tagImage(ctx, cli, t.ImageTag, appName, newDeploymentID)
			if err != nil {
				return fmt.Errorf("failed to tag image: %w", err)
			}
			ui.Info("Creating new deployment from image %s", t.ImageTag)
			appConfig := t.AppConfig
			if appConfig == nil {
				ui.Warn("Could not find old app config for %s, trying current config: %v", appName, err)
				loadedAppConfig, err := config.AppConfigByName(appName)
				if err != nil {
					return fmt.Errorf("failed to load app config for %s: %w", appName, err)
				}
				appConfig = loadedAppConfig
			}
			if err := DeployApp(ctx, cli, appConfig, newImageTag); err != nil {
				return fmt.Errorf("failed to deploy app %s: %w", appName, err)
			}

			// found the target, break the loop
			break
		}
	}

	return nil
}

// getRollbackTargets retrieves and sorts all available rollback targets for the specified app.
func GetRollbackTargets(ctx context.Context, cli *client.Client, appName string) (targets []RollbackTarget, err error) {
	if appName == "" {
		return targets, fmt.Errorf("app name cannot be empty")
	}

	// Get avaiable images for the app
	// List all images for the app that match the format appName:<deploymentID>.
	images, err := cli.ImageList(ctx, image.ListOptions{
		Filters: filters.NewArgs(filters.Arg("reference", appName+":*")),
	})
	if err != nil {
		return targets, fmt.Errorf("failed to list images for %s: %w", appName, err)
	}
	var latestImageID string
	for _, img := range images {
		for _, tag := range img.RepoTags {
			if strings.HasSuffix(tag, ":latest") {
				latestImageID = img.ID
				continue
			}
			// Expected tag format: "appName:deploymentID", e.g. "test-app:20250615214304"
			parts := strings.SplitN(tag, ":", 2)
			if len(parts) != 2 {
				// Unexpected tag format, skip this tag.
				continue
			}
			deploymentID := parts[1]
			isLatest := img.ID == latestImageID
			appConfig, _ := GetAppConfigHistory(deploymentID)
			target := RollbackTarget{
				DeploymentID: deploymentID,
				ImageID:      img.ID,
				ImageTag:     tag,
				IsLatest:     isLatest,
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
	// List all ContainerList for the app
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
		return deploymentIDs[i] > deploymentIDs[j] // Newest first
	})

	return deploymentIDs[0], nil
}
