package haloy

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ameistad/haloy/internal/apiclient"
	"github.com/ameistad/haloy/internal/cmdexec"
	"github.com/ameistad/haloy/internal/config"
	"github.com/ameistad/haloy/internal/ui"
)

func ResolveImageBuilds(targets []config.AppConfig) (map[string]*config.Image, map[string][]*config.AppConfig) {
	builds := make(map[string]*config.Image)        // key is imageRef
	uploads := make(map[string][]*config.AppConfig) // key is imageRef

	for _, target := range targets {
		image := target.Image
		if image == nil || image.Builder == nil {
			continue
		}

		imageRef := image.ImageRef()

		if _, exists := builds[imageRef]; !exists {
			builds[imageRef] = image
		}

		if image.Builder.UploadToServer {
			uploads[imageRef] = append(uploads[imageRef], &target)
		}
	}

	return builds, uploads
}

// BuildImage builds a Docker image using the provided image configuration
func BuildImage(ctx context.Context, imageRef string, image *config.Image, configPath string) error {
	if image.Builder == nil {
		return fmt.Errorf("no builder configuration found for image %s", imageRef)
	}

	builder := image.Builder
	workDir := getBuilderWorkDir(configPath, builder.Context)

	ui.Info("Building image %s", imageRef)

	// Build the docker build command
	args := []string{"build"}

	// Add build context (defaults to current directory)
	buildContext := "."
	if builder.Context != "" {
		buildContext = builder.Context
	}

	// Add dockerfile if specified
	if builder.Dockerfile != "" {
		args = append(args, "-f", builder.Dockerfile)
	}

	// Add platform if specified
	if builder.Platform != "" {
		args = append(args, "--platform", builder.Platform)
	}

	// Add build args
	for _, buildArg := range builder.Args {
		if buildArg.Value != "" {
			args = append(args, "--build-arg", fmt.Sprintf("%s=%s", buildArg.Name, buildArg.Value))
		} else {
			// If no value specified, pass the build arg name only (Docker will use env var)
			args = append(args, "--build-arg", buildArg.Name)
		}
	}

	// Add image tag
	args = append(args, "-t", imageRef)

	// Add build context as the last argument
	args = append(args, buildContext)

	// Execute the docker build command
	cmd := fmt.Sprintf("docker %s", strings.Join(args, " "))
	if err := cmdexec.RunCommand(ctx, cmd, workDir); err != nil {
		return fmt.Errorf("failed to build image %s: %w", imageRef, err)
	}

	ui.Success("Successfully built image %s", imageRef)
	return nil
}

// getBuilderWorkDir determines the working directory for the docker build command
func getBuilderWorkDir(configPath, builderContext string) string {
	workDir := "."

	// Start with the config file's directory
	if configPath != "." {
		if stat, err := os.Stat(configPath); err == nil {
			if stat.IsDir() {
				workDir = configPath
			} else {
				workDir = filepath.Dir(configPath)
			}
		}
	}

	// If builder.Context is an absolute path, use it as-is
	// If it's relative, it should be relative to the config file's directory
	if builderContext != "" && filepath.IsAbs(builderContext) {
		workDir = filepath.Dir(builderContext)
	}

	return workDir
}

// UploadImage uploads a Docker image tar to the specified server
func UploadImage(ctx context.Context, imageRef string, resolvedAppConfig config.AppConfig) error {
	tempFile, err := os.CreateTemp("", fmt.Sprintf("haloy-upload-%s-*.tar", strings.ReplaceAll(imageRef, ":", "-")))
	if err != nil {
		return fmt.Errorf("failed to create temporary file: %w", err)
	}
	defer os.Remove(tempFile.Name())
	defer tempFile.Close()

	ui.Info("Saving image %s to tar file", imageRef)

	// Save image to tar
	saveCmd := fmt.Sprintf("docker save -o %s %s", tempFile.Name(), imageRef)
	if err := cmdexec.RunCommand(ctx, saveCmd, "."); err != nil {
		return fmt.Errorf("failed to save image to tar: %w", err)
	}

	ui.Info("Uploading image %s to server", imageRef)

	// Get token and create API client
	token, err := getToken(&resolvedAppConfig, resolvedAppConfig.Server)
	if err != nil {
		return fmt.Errorf("failed to get authentication token: %w", err)
	}

	api, err := apiclient.NewWithTimeout(resolvedAppConfig.Server, token, 5*time.Minute)
	if err != nil {
		return fmt.Errorf("failed to create API client: %w", err)
	}

	// Upload the tar file
	if err := api.PostFile(ctx, "images/upload", "image", tempFile.Name()); err != nil {
		return fmt.Errorf("failed to upload image: %w", err)
	}

	ui.Success("Successfully uploaded image %s to server", imageRef)
	return nil
}
