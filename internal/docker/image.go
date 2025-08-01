package docker

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"sort"
	"strings"

	"github.com/ameistad/haloy/internal/config"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/client"
)

func EnsureImageUpToDate(ctx context.Context, cli *client.Client, logger *slog.Logger, imageConfig config.Image) (imageName string, err error) {
	imageName = imageConfig.Repository
	imageRef := imageConfig.ImageRef()
	registryAuth, err := imageConfig.RegistryAuthString()
	if err != nil {
		return imageName, fmt.Errorf("failed to resolve registry auth for image %s: %w", imageName, err)
	}
	// Try inspecting local image
	local, err := cli.ImageInspect(ctx, imageName)
	if err == nil {
		// Inspect remote manifest (HEAD)
		remote, err := cli.DistributionInspect(ctx, imageRef, registryAuth)
		if err == nil {
			remoteDigest := remote.Descriptor.Digest.String()
			for _, rd := range local.RepoDigests {
				// rd is "repo@sha256:..."
				if strings.HasSuffix(rd, "@"+remoteDigest) {
					return imageName, nil
				}
			}
		}
	}
	// If we reach here, either the image doesn't exist locally or the remote digest doesn't match
	logger.Info(fmt.Sprintf("Pulling image %s...", imageName))
	r, err := cli.ImagePull(ctx, imageName, image.PullOptions{
		RegistryAuth: registryAuth,
	})
	if err != nil {
		return imageName, fmt.Errorf("failed to pull %s: %w", imageName, err)
	}
	defer r.Close()
	// drain stream
	if _, err := io.Copy(io.Discard, r); err != nil {
		return imageName, fmt.Errorf("error reading pull response: %w", err)
	}
	logger.Info("Successfully pulled image", "image", imageName)
	return imageName, nil
}

// PruneImages removes dangling (unused) Docker images and returns the amount of space reclaimed.
func PruneImages(ctx context.Context, cli *client.Client, logger *slog.Logger) (uint64, error) {
	report, err := cli.ImagesPrune(ctx, filters.Args{})
	if err != nil {
		return 0, fmt.Errorf("failed to prune images: %w", err)
	}
	if len(report.ImagesDeleted) > 0 {
		logger.Info("Pruned images", "count", len(report.ImagesDeleted), "bytes_reclaimed", report.SpaceReclaimed)
	}
	return report.SpaceReclaimed, nil
}

// RemoveImages removes extra (duplicate) image tags for a given app, keeping only the newest N tags based on the deploymentID.
// Running containers reference the image by digest; if an image is in use we allow removal of duplicate tags as long as at least one tag is preserved.
func RemoveImages(ctx context.Context, cli *client.Client, logger *slog.Logger, appName, ignoreDeploymentID string, deploymentsToKeep int) error {
	// List all images for the app that match the format appName:<deploymentID>.
	images, err := cli.ImageList(ctx, image.ListOptions{
		Filters: filters.NewArgs(filters.Arg("reference", appName+":*")),
	})
	if err != nil {
		return fmt.Errorf("failed to list images for %s: %w", appName, err)
	}

	// Get a list of running containers for the app to determine which image digests are currently in use.
	containerList, err := GetAppContainers(ctx, cli, false, appName)
	if err != nil {
		return err
	}
	// Build a set of imageIDs that are in use.
	inUseImageIDs := make(map[string]struct{})
	for _, container := range containerList {
		if container.ImageID != "" {
			inUseImageIDs[container.ImageID] = struct{}{}
		}
	}

	// Build a candidate list of removable image tags.
	// Only consider tags that are not ":latest" and that start with the appName prefix.
	type removeImage struct {
		Tag          string
		DeploymentID string
		ImageID      string
	}
	var candidates []removeImage
	for _, img := range images {
		for _, tag := range img.RepoTags {
			// Skip the "latest" and ignoreDeploymentID tag and any tag not matching the expected format.
			if strings.HasSuffix(tag, ":latest") || strings.HasSuffix(tag, ":"+ignoreDeploymentID) || !strings.HasPrefix(tag, appName+":") {
				continue
			}
			// Expected tag format: "appName:deploymentID", e.g. "test-app:20250615214304"
			parts := strings.SplitN(tag, ":", 2)
			if len(parts) != 2 {
				// Unexpected tag format, skip this tag.
				continue
			}
			deploymentID := parts[1]
			candidates = append(candidates, removeImage{
				Tag:          tag,
				DeploymentID: deploymentID,
				ImageID:      img.ID,
			})
		}
	}

	// Sort the candidate tags descending by deploymentID (newest first).
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].DeploymentID > candidates[j].DeploymentID
	})

	// Build sets of safe-to-keep tags and the corresponding imageIDs.
	keepTags := make(map[string]struct{})
	keepImageIDs := make(map[string]struct{})
	for i, cand := range candidates {
		if i < deploymentsToKeep {
			keepTags[cand.Tag] = struct{}{}
			keepImageIDs[cand.ImageID] = struct{}{}
		}
	}

	// Remove duplicate tags that are not marked to keep.
	// For images in use we only remove extra tags when at least one tag is kept.
	for _, cand := range candidates {
		// Skip candidate if its tag is in the keep set.
		if _, ok := keepTags[cand.Tag]; ok {
			continue
		}
		// Check: if the image is in use and its digest is not in the keep list,
		// then don't remove this tag to ensure at least one tag remains for running containers.
		_, inUse := inUseImageIDs[cand.ImageID]
		_, idInKeep := keepImageIDs[cand.ImageID]
		if inUse && !idInKeep {
			continue
		}

		// Remove the candidate tag.
		if _, err := cli.ImageRemove(ctx, cand.Tag, image.RemoveOptions{Force: true, PruneChildren: false}); err != nil {
			logger.Error("Failed to remove image tag", "tag", cand.Tag, "error", err)
		} else {
			logger.Info("Removed image tag", "tag", cand.Tag)
		}
	}

	return nil
}
