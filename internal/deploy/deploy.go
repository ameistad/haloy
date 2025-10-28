package deploy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/ameistad/haloy/internal/config"
	"github.com/ameistad/haloy/internal/docker"
	"github.com/ameistad/haloy/internal/storage"
	"github.com/docker/docker/client"
)

func DeployApp(ctx context.Context, cli *client.Client, deploymentID string, targetConfig config.TargetConfig, rawAppConfig config.AppConfig, logger *slog.Logger) error {
	imageRef := targetConfig.Image.ImageRef()

	err := docker.EnsureImageUpToDate(ctx, cli, logger, *targetConfig.Image)
	if err != nil {
		return err
	}

	newImageRef, err := tagImage(ctx, cli, imageRef, targetConfig.Name, deploymentID)
	if err != nil {
		return fmt.Errorf("failed to tag image: %w", err)
	}

	runResult, err := docker.RunContainer(ctx, cli, deploymentID, newImageRef, targetConfig)
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
	// We'll make sure to save the raw app config (without resolved secrets to history)
	handleImageHistory(ctx, cli, rawAppConfig, deploymentID, newImageRef, logger)

	return nil
}

func handleImageHistory(ctx context.Context, cli *client.Client, rawAppConfig config.AppConfig, deploymentID, newImageRef string, logger *slog.Logger) {
	image := rawAppConfig.Image

	if image == nil {
		logger.Debug("No image configuration found, skipping history management")
		return
	}

	strategy := config.HistoryStrategyLocal
	if image.History != nil {
		strategy = image.History.Strategy
	}

	switch strategy {
	case config.HistoryStrategyNone:
		logger.Debug("History disabled, skipping cleanup and history storage")

	case config.HistoryStrategyLocal:
		if err := writeAppConfigHistory(rawAppConfig, deploymentID, newImageRef); err != nil {
			logger.Warn("Failed to write app config history", "error", err)
		} else {
			logger.Debug("App configuration saved to history")
		}

		// Keep N images locally for fast rollback
		if err := docker.RemoveImages(ctx, cli, logger, rawAppConfig.Name, deploymentID, *rawAppConfig.Image.History.Count); err != nil {
			logger.Warn("Failed to clean up old images", "error", err)
		} else {
			logger.Debug(fmt.Sprintf("Old images cleaned up, keeping %d recent images locally", *rawAppConfig.Image.History.Count))
		}

	case config.HistoryStrategyRegistry:
		// Save deployment history for rollback metadata
		if err := writeAppConfigHistory(rawAppConfig, deploymentID, newImageRef); err != nil {
			logger.Warn("Failed to write app config history", "error", err)
		} else {
			logger.Debug("App configuration saved to history")
		}

		// Remove all old images - registry is source of truth
		// Keep only the current deployment's image (count = 1)
		if err := docker.RemoveImages(ctx, cli, logger, rawAppConfig.Name, deploymentID, 1); err != nil {
			logger.Warn("Failed to clean up old images", "error", err)
		} else {
			logger.Debug("Old images cleaned up, registry strategy - keeping only current image locally")
		}

	default:
		logger.Warn("Unknown history strategy, skipping history management", "strategy", rawAppConfig.Image.History.Strategy)
	}
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

// writeAppConfigHistory writes the given appConfig to the db. It will save the newImageRef as a json repsentation of the Image struct to use for rollbacks
func writeAppConfigHistory(rawAppConfig config.AppConfig, deploymentID, newImageRef string) error {
	if rawAppConfig.Image.History == nil {
		return fmt.Errorf("image.history must be set")
	}

	if rawAppConfig.Image.History.Strategy != config.HistoryStrategyNone && rawAppConfig.Image.History.Count == nil {
		return fmt.Errorf("image.history.count is required for %s strategy", rawAppConfig.Image.History.Strategy)
	}

	db, err := storage.New()
	if err != nil {
		return err
	}
	defer db.Close()

	rawAppConfigJSON, err := json.Marshal(rawAppConfig)
	if err != nil {
		return fmt.Errorf("failed to convert target config to JSON: %w", err)
	}

	deployedImage := rawAppConfig.Image
	if parts := strings.SplitN(newImageRef, ":", 2); len(parts) == 2 {
		deployedImage.Repository = parts[0]
		deployedImage.Tag = parts[1]
	}

	deployedImageJSON, err := json.Marshal(deployedImage)
	if err != nil {
		return fmt.Errorf("failed to convert deployed image to JSON: %w", err)
	}

	deployment := storage.Deployment{
		ID:            deploymentID,
		AppName:       rawAppConfig.Name,
		RawAppConfig:  rawAppConfigJSON,
		DeployedImage: deployedImageJSON,
	}

	if err := db.SaveDeployment(deployment); err != nil {
		return fmt.Errorf("failed to save deployment to database: %w", err)
	}

	if err := db.PruneOldDeployments(rawAppConfig.Name, *rawAppConfig.Image.History.Count); err != nil {
		return fmt.Errorf("failed to prune old deployments: %w", err)
	}

	return nil
}
