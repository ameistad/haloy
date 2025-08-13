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
	now := time.Now()
	// Use seconds precision + last 2 digits of nanoseconds for uniqueness
	timestamp := now.Format("20060102150405")
	nanos := fmt.Sprintf("%02d", (now.Nanosecond()/10000000)%100) // Last 2 digits of centiseconds
	return timestamp + nanos
}
func DeployApp(ctx context.Context, cli *client.Client, deploymentID string, appConfig config.AppConfig, logger *slog.Logger) error {
	normalizedAppConfig := appConfig.Normalize()
	if err := normalizedAppConfig.Validate(); err != nil {
		return fmt.Errorf("app config validation failed: %w", err)
	}
	imageRef := appConfig.Image.ImageRef()

	err := docker.EnsureImageUpToDate(ctx, cli, logger, appConfig.Image)
	if err != nil {
		return err
	}
	newImageRef, err := tagImage(ctx, cli, imageRef, appConfig.Name, deploymentID)
	if err != nil {
		return fmt.Errorf("failed to tag image: %w", err)
	}

	runResult, err := docker.RunContainer(ctx, cli, deploymentID, newImageRef, appConfig)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return fmt.Errorf("container startup timed out: %w", err)
		} else if errors.Is(err, context.Canceled) {
			logger.Warn("Deployment canceled", "error", err)
			if ctx.Err() != nil {
				return fmt.Errorf("deployment canceled: %w", ctx.Err())
			}
			return fmt.Errorf("container creation canceled: %w", err)
		}
		return err
	}
	if len(runResult) == 0 {
		return fmt.Errorf("no containers started, check logs for details")
	} else if len(runResult) == 1 {
		logger.Info("Container started successfully", "containerID", runResult[0].ID, "deploymentID", deploymentID)
	} else {
		logger.Info(fmt.Sprintf("Containers started successfully (%d replicas)", len(runResult)), "count", len(runResult), "deploymentID", deploymentID)
	}
	if err := writeAppConfigHistory(appConfig, deploymentID, newImageRef, *appConfig.DeploymentsToKeep); err != nil {
		logger.Warn("Failed to write app config history", "error", err)
	} else {
		logger.Debug("App configuration saved to history")
	}

	// Remove all images except the DeploymentsToKeep newest, the ones tagged as latest and in use.
	if err := docker.RemoveImages(ctx, cli, logger, appConfig.Name, deploymentID, *appConfig.DeploymentsToKeep); err != nil {
	} else {
		logger.Debug("Old images cleaned up")
	}
	return nil
}

func tagImage(ctx context.Context, cli *client.Client, srcRef, appName, deploymentID string) (string, error) {
	dstRef := fmt.Sprintf("%s:%s", appName, deploymentID)

	if srcRef == dstRef {
		return dstRef, nil
	}

	if err := cli.ImageTag(ctx, srcRef, dstRef); err != nil {
		return dstRef, fmt.Errorf("tag image: %w", err)
	}
	return dstRef, nil
}
