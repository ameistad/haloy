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

func ResolveImageBuilds(targets []config.AppConfig) (map[string]*config.Image, map[string][]*config.Image, map[string][]*config.AppConfig) {
	builds := make(map[string]*config.Image) // imageRef is key
	uploads := make(map[string][]*config.AppConfig)
	pushes := make(map[string][]*config.Image)

	for _, target := range targets {
		image := target.Image
		if image == nil || !image.ShouldBuild() {
			continue
		}

		imageRef := image.ImageRef()

		if _, exists := builds[imageRef]; !exists {
			builds[imageRef] = image
		}

		// Get push strategy with safe access
		pushStrategy := config.BuildPushOptionRegistry // default
		if image.BuildConfig != nil && image.BuildConfig.Push != "" {
			pushStrategy = image.BuildConfig.Push
		}

		if pushStrategy == config.BuildPushOptionServer {
			uploads[imageRef] = append(uploads[imageRef], &target)
		} else if pushStrategy == config.BuildPushOptionRegistry && image.RegistryAuth != nil {
			pushes[imageRef] = append(pushes[imageRef], target.Image)
		}
	}

	return builds, pushes, uploads
}

// BuildImage builds a Docker image using the provided image configuration
func BuildImage(ctx context.Context, imageRef string, image *config.Image, configPath string) error {
	ui.Info("Building image %s", imageRef)

	buildConfig := image.BuildConfig
	if buildConfig == nil {
		buildConfig = &config.BuildConfig{}
	}
	workDir := getBuilderWorkDir(configPath, buildConfig.Context)

	buildContext := "."
	if buildConfig.Context != "" {
		buildContext = buildConfig.Context
	}

	args := []string{"build"}

	if buildConfig.Dockerfile != "" {
		args = append(args, "-f", buildConfig.Dockerfile)
	}

	if buildConfig.Platform == "" {
		buildConfig.Platform = "linux/amd64" // most widely used platform and a common pitfall
	}
	args = append(args, "--platform", buildConfig.Platform)

	for _, buildArg := range buildConfig.Args {
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

	if configPath != "." {
		if stat, err := os.Stat(configPath); err == nil {
			if stat.IsDir() {
				workDir = configPath
			} else {
				workDir = filepath.Dir(configPath)
			}
		}
	}

	if builderContext != "" {
		if filepath.IsAbs(builderContext) {
			// For absolute paths, use the path directly as working directory
			workDir = builderContext
		} else {
			// For relative paths, combine with config directory
			workDir = filepath.Join(workDir, builderContext)
		}
	}

	return workDir
}

// UploadImage uploads a Docker image tar to the specified server
func UploadImage(ctx context.Context, imageRef string, resolvedAppConfigs []*config.AppConfig) error {
	tempFile, err := os.CreateTemp("", fmt.Sprintf("haloy-upload-%s-*.tar", strings.ReplaceAll(imageRef, ":", "-")))
	if err != nil {
		return fmt.Errorf("failed to create temporary file: %w", err)
	}
	defer os.Remove(tempFile.Name())
	defer tempFile.Close()

	saveCmd := fmt.Sprintf("docker save -o %s %s", tempFile.Name(), imageRef)
	if err := cmdexec.RunCommand(ctx, saveCmd, "."); err != nil {
		return fmt.Errorf("failed to save image to tar: %w", err)
	}

	for _, resolvedAppConfig := range resolvedAppConfigs {
		ui.Info("Uploading image %s to %s", imageRef, resolvedAppConfig.TargetName)

		token, err := getToken(resolvedAppConfig, resolvedAppConfig.Server)
		if err != nil {
			return fmt.Errorf("failed to get authentication token: %w", err)
		}

		api, err := apiclient.NewWithTimeout(resolvedAppConfig.Server, token, 5*time.Minute)
		if err != nil {
			return fmt.Errorf("failed to create API client: %w", err)
		}

		if err := api.PostFile(ctx, "images/upload", "image", tempFile.Name()); err != nil {
			return fmt.Errorf("failed to upload image: %w", err)
		}

		ui.Success("Successfully uploaded image %s to server", imageRef)
	}

	return nil
}
