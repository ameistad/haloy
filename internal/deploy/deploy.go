package deploy

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/ameistad/haloy/internal/config"
	"github.com/ameistad/haloy/internal/docker"
	"github.com/docker/docker/client"
)

func CreateDeploymentID() string {
	// Generate a unique deployment ID based on the current time.
	// This format is YYYYMMDDHHMMSS, which is sortable and unique.
	return time.Now().Format("20060102150405")
}

func DeployApp(ctx context.Context, cli *client.Client, deploymentID string, appConfig config.AppConfig, logger *slog.Logger) error {
	normalizedAppConfig := appConfig.Normalize()
	if err := normalizedAppConfig.Validate(); err != nil {
		return fmt.Errorf("app config validation failed: %w", err)
	}

	imageRef := appConfig.Image.ImageRef()
	newImageTag, err := tagImage(ctx, cli, imageRef, appConfig.Name, deploymentID)
	if err != nil {
		return fmt.Errorf("failed to tag image: %w", err)
	}

	runResult, err := docker.RunContainer(ctx, cli, deploymentID, newImageTag, appConfig)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return fmt.Errorf("failed to run new container: operation timed out (%w)", err)
		} else if errors.Is(err, context.Canceled) {
			logger.Warn("Deployment canceled", "error", err)
			if ctx.Err() != nil {
				return fmt.Errorf("failed to run new container: deployment canceled (%w)", ctx.Err())
			}
			return fmt.Errorf("failed to run new container: docker operation canceled (%w)", err)
		}
		return fmt.Errorf("failed to run new container: %w", err)
	}
	if len(runResult) == 0 {
		return fmt.Errorf("failed to run new container: no containers started")
	}

	logger.Info("Container(s) started successfully", "count", len(runResult), "deploymentID", deploymentID)

	// Write the app configuration to the history folder.
	if err := writeAppConfigHistory(appConfig, deploymentID, newImageTag, *appConfig.DeploymentsToKeep); err != nil {
		logger.Warn("Failed to write app config history", "error", err)
	} else {
		logger.Info("App configuration saved to history")
	}

	// Remove all images except the DeploymentsToKeep newest, the ones tagged as latest and in use.
	if err := docker.RemoveImages(ctx, cli, logger, appConfig.Name, deploymentID, *appConfig.DeploymentsToKeep); err != nil {
	} else {
		logger.Info("Old images cleaned up")
	}
	return nil
}

func tagImage(ctx context.Context, cli *client.Client, srcRef, appName, deploymentID string) (string, error) {
	dstRef := fmt.Sprintf("%s:%s", appName, deploymentID)

	if srcRef == dstRef { // already tagged
		return dstRef, nil
	}

	if err := cli.ImageTag(ctx, srcRef, dstRef); err != nil {
		return dstRef, fmt.Errorf("tag image: %w", err)
	}
	return dstRef, nil
}
