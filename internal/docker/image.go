package docker

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ameistad/haloy/internal/config"
	"github.com/ameistad/haloy/internal/ui"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/client"
)

func EnsureImageUpToDate(ctx context.Context, dockerClient *client.Client, imageSource *config.ImageSource) (imageName string, err error) {
	imageName = imageSource.Repository
	imageRef := imageSource.ImageRef()
	registryAuth, err := imageSource.RegistryAuthString()
	if err != nil {
		return imageName, fmt.Errorf("failed to resolve registry auth for image %s: %w", imageName, err)
	}
	// Try inspecting local image
	local, err := dockerClient.ImageInspect(ctx, imageName)
	if err == nil {
		// Inspect remote manifest (HEAD)
		remote, err := dockerClient.DistributionInspect(ctx, imageRef, registryAuth)
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
	ui.Info("Pulling image %s...", imageName)
	r, err := dockerClient.ImagePull(ctx, imageName, image.PullOptions{
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
	ui.Info("Successfully pulled %s", imageName)
	return imageName, nil
}

type BuildImageParams struct {
	Context   context.Context
	ImageName string
	Source    *config.DockerfileSource
	EnvVars   []config.EnvVar
}

func BuildImage(params BuildImageParams) error {

	absContext, err := filepath.Abs(params.Source.BuildContext)
	if err != nil {
		return fmt.Errorf("failed to resolve absolute path for build context '%s': %w", params.Source.BuildContext, err)
	}
	absDockerfile, err := filepath.Abs(params.Source.Path)
	if err != nil {
		return fmt.Errorf("failed to resolve absolute path for Dockerfile '%s': %w", params.Source.Path, err)
	}

	// Docker CLI expects the Dockerfile path to be relative to the build context if it's inside.
	dockerfilePathForCli := absDockerfile
	if strings.HasPrefix(absDockerfile, absContext+string(filepath.Separator)) {
		relPath, err := filepath.Rel(absContext, absDockerfile)
		if err != nil {
			return fmt.Errorf("failed to calculate relative Dockerfile path for CLI: %w", err)
		}
		dockerfilePathForCli = relPath
	}

	cmdArgs := []string{"build", "-t", params.ImageName, "-f", dockerfilePathForCli}

	// Add build args from params.Source.BuildArgs
	for k, v := range params.Source.BuildArgs {
		cmdArgs = append(cmdArgs, "--build-arg", fmt.Sprintf("%s=%s", k, v))
	}

	// Add environment variables as build args
	if len(params.EnvVars) > 0 {
		decryptedEnvVars, err := config.DecryptEnvVars(params.EnvVars)
		if err != nil {
			return fmt.Errorf("failed to decrypt env vars for build args: %w", err)
		}
		for _, envVar := range decryptedEnvVars {
			strValue, err := envVar.GetValue() // Assuming GetValue returns the decrypted string
			if err != nil {
				return fmt.Errorf("failed to get value for environment variable '%s': %w", envVar.Name, err)
			}
			cmdArgs = append(cmdArgs, "--build-arg", fmt.Sprintf("%s=%s", envVar.Name, strValue))
		}
	}

	// Add the build context path at the end
	cmdArgs = append(cmdArgs, absContext)

	cmd := exec.CommandContext(params.Context, "docker", cmdArgs...)
	cmd.Dir = absContext // Set the working directory for the command

	// For real-time output, you might want to use cmd.StdoutPipe and cmd.StderrPipe
	// and process them in goroutines. For simplicity, CombinedOutput is used here.
	output, err := cmd.CombinedOutput()

	if err != nil {
		// Log the output for debugging
		ui.Debug("Docker build failed. Output:\n%s", string(output))

		if exitErr, ok := err.(*exec.ExitError); ok {
			return fmt.Errorf("docker build command failed with exit code %d: %w. Output: %s", exitErr.ExitCode(), err, string(output))
		}
		return fmt.Errorf("failed to execute docker build command: %w. Output: %s", err, string(output))
	}

	return nil
}

// PruneImages removes dangling (unused) Docker images and returns the amount of space reclaimed.
func PruneImages(ctx context.Context, dockerClient *client.Client) (uint64, error) {
	report, err := dockerClient.ImagesPrune(ctx, filters.Args{})
	if err != nil {
		return 0, fmt.Errorf("failed to prune images: %w", err)
	}
	if len(report.ImagesDeleted) > 0 {
		ui.Info("Pruned %d images, reclaimed %d bytes", len(report.ImagesDeleted), report.SpaceReclaimed)
	}
	return report.SpaceReclaimed, nil
}

// RemoveImages removes extra (duplicate) image tags for a given app, keeping only the newest N tags based on the deploymentID.
// Running containers reference the image by digest; if an image is in use we allow removal of duplicate tags as long as at least one tag is preserved.
func RemoveImages(ctx context.Context, cli *client.Client, appName, ignoreDeploymentID string, deploymentsToKeep int) error {
	// List all images for the app that match the format appName:<deploymentID>.
	images, err := cli.ImageList(ctx, image.ListOptions{
		Filters: filters.NewArgs(filters.Arg("reference", appName+":*")),
	})
	if err != nil {
		return fmt.Errorf("failed to list images for %s: %w", appName, err)
	}

	// Get a list of running containers for the app to determine which image digests are currently in use.
	filterArgs := filters.NewArgs(filters.Arg("label", fmt.Sprintf("%s=%s", config.LabelAppName, appName)))
	containerList, err := cli.ContainerList(ctx, container.ListOptions{
		Filters: filterArgs,
		All:     false, // Only consider running containers.
	})
	if err != nil {
		return fmt.Errorf("failed to list containers for %s: %w", appName, err)
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
			ui.Error("Failed to remove image tag %s: %v", cand.Tag, err)
		} else {
			ui.Info("Removed image %s", cand.Tag)
		}
	}

	return nil
}
