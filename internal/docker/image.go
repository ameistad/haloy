package docker

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/ameistad/haloy/internal/config"
	"github.com/ameistad/haloy/internal/ui"

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
					ui.Info("Image %s is already at latest (%s)", imageName, remoteDigest)
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
	ui.Success("Pulled %s", imageName)
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

	ui.Success("Docker build completed successfully for '%s'.", params.ImageName)

	return nil
}
