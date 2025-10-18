package docker

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sort"
	"strings"

	"github.com/ameistad/haloy/internal/config"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/registry"
	"github.com/docker/docker/client"
)

func getRegistryAuthString(imageConfig *config.Image) (string, error) {
	auth := imageConfig.RegistryAuth
	if auth == nil {
		return "", nil
	}
	server := "index.docker.io" // Default to Docker Hub if no server specified
	if auth.Server != "" {
		server = auth.Server
	} else {
		// If no server is set, parse it from the Repository field
		parts := strings.SplitN(imageConfig.Repository, "/", 2)
		if len(parts) > 1 && strings.Contains(parts[0], ".") {
			server = parts[0]
		}
	}
	authConfig := registry.AuthConfig{
		Username:      auth.Username.Value,
		Password:      auth.Password.Value,
		ServerAddress: server,
	}
	authStr, err := registry.EncodeAuthConfig(authConfig)
	if err != nil {
		return "", err
	}
	return authStr, nil
}

func EnsureImageUpToDate(ctx context.Context, cli *client.Client, logger *slog.Logger, imageConfig config.Image) error {
	imageRef := imageConfig.ImageRef()

	local, err := cli.ImageInspect(ctx, imageRef)
	localExists := (err == nil)

	// If Builder.UploadToServer is set to true, we'll assume the local image should be used.
	if imageConfig.Builder != nil && imageConfig.Builder.UploadToServer {
		if !localExists {
			return fmt.Errorf("uploaded image '%s' not found", imageRef)
		}
		logger.Debug("Using local image", "image", imageRef)
		return nil
	}

	registryAuth, err := getRegistryAuthString(&imageConfig)
	if err != nil {
		return fmt.Errorf("failed to resolve registry auth for image %s: %w", imageRef, err)
	}

	if localExists {
		remote, err := cli.DistributionInspect(ctx, imageRef, registryAuth)
		if err != nil {
			// Registry check failed - this is an error for registry images
			return fmt.Errorf("failed to check remote registry for image %s: %w", imageRef, err)
		}

		remoteDigest := remote.Descriptor.Digest.String()
		if local.RepoDigests != nil {
			for _, rd := range local.RepoDigests {
				if strings.HasSuffix(rd, "@"+remoteDigest) {
					logger.Debug("Registry image is up to date", "image", imageRef)
					return nil // Local matches remote - use local
				}
			}
		}
		logger.Debug("Local image outdated, pulling from registry", "image", imageRef)
	}

	// If we reach here, either the image doesn't exist locally or the remote digest doesn't match
	logger.Debug(fmt.Sprintf("Pulling image %s...", imageRef), "image", imageRef)
	r, err := cli.ImagePull(ctx, imageRef, image.PullOptions{
		RegistryAuth: registryAuth,
	})
	if err != nil {
		return fmt.Errorf("failed to pull %s: %w", imageRef, err)
	}
	defer r.Close()
	// drain stream
	if _, err := io.Copy(io.Discard, r); err != nil {
		return fmt.Errorf("error reading pull response: %w", err)
	}
	logger.Debug("Successfully pulled image", "image", imageRef)
	return nil
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
			logger.Debug("Removed image tag", "tag", cand.Tag)
		}
	}

	return nil
}

func LoadImageFromTar(ctx context.Context, cli *client.Client, tarPath string) error {
	fmt.Printf("=== DOCKER LOAD DEBUG START ===\n")
	fmt.Printf("Loading image from tar: %s\n", tarPath)

	// Check if file exists and is readable
	if stat, err := os.Stat(tarPath); err != nil {
		fmt.Printf("Tar file stat failed: %v\n", err)
		return fmt.Errorf("tar file not accessible: %w", err)
	} else {
		fmt.Printf("Tar file exists, size: %d bytes\n", stat.Size())
	}

	file, err := os.Open(tarPath)
	if err != nil {
		fmt.Printf("Failed to open tar file: %v\n", err)
		return fmt.Errorf("failed to open tar file: %w", err)
	}
	defer file.Close()
	fmt.Printf("Tar file opened successfully\n")

	response, err := cli.ImageLoad(ctx, file, false)
	if err != nil {
		fmt.Printf("ImageLoad failed: %v\n", err)
		return fmt.Errorf("failed to load image: %w", err)
	}
	defer response.Body.Close()
	fmt.Printf("ImageLoad call completed\n")

	// Read and log the response - this is crucial!
	body, err := io.ReadAll(response.Body)
	if err != nil {
		fmt.Printf("Failed to read response body: %v\n", err)
		return fmt.Errorf("failed to read load response: %w", err)
	}

	responseText := string(body)
	fmt.Printf("Docker load response length: %d\n", len(responseText))
	fmt.Printf("Docker load response: '%s'\n", responseText)

	// Parse the response line by line to see what images were loaded
	lines := strings.Split(responseText, "\n")
	fmt.Printf("Response has %d lines:\n", len(lines))
	loadedImages := []string{}
	for i, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			fmt.Printf("  Line %d: '%s'\n", i, line)
			// Look for "Loaded image:" messages
			if strings.HasPrefix(line, "Loaded image:") {
				loadedImage := strings.TrimSpace(strings.TrimPrefix(line, "Loaded image:"))
				loadedImages = append(loadedImages, loadedImage)
			}
		}
	}

	if len(loadedImages) == 0 {
		fmt.Printf("WARNING: No 'Loaded image:' messages found in Docker response\n")
		return fmt.Errorf("no images were loaded from tar file - Docker response: %s", responseText)
	}

	fmt.Printf("Successfully loaded images: %v\n", loadedImages)
	fmt.Printf("=== DOCKER LOAD DEBUG END ===\n")

	return nil
}
