package docker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ameistad/haloy/internal/config"
	"github.com/ameistad/haloy/internal/ui"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/archive"
	"github.com/docker/docker/pkg/jsonmessage"
)

type BuildImageParams struct {
	Context      context.Context
	DockerClient *client.Client
	ImageName    string
	Source       *config.DockerfileSource
	EnvVars      []config.EnvVar
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

	// Check if Dockerfile is within the build context.
	isDockerfileInContext := strings.HasPrefix(absDockerfile, absContext+string(filepath.Separator)) || absDockerfile == absContext

	// Calculate Dockerfile path for Docker API.
	var dockerfilePath string
	if isDockerfileInContext {
		relPath, err := filepath.Rel(absContext, absDockerfile)
		if err != nil {
			return fmt.Errorf("failed to calculate relative Dockerfile path: %w", err)
		}
		dockerfilePath = relPath
	} else {
		dockerfilePath = filepath.Base(absDockerfile)
	}

	buildOpts := types.ImageBuildOptions{
		Tags:       []string{params.ImageName},
		Dockerfile: dockerfilePath,
		BuildArgs:  make(map[string]*string),
		Remove:     true,
		Version:    types.BuilderBuildKit,
		// Uncomment this line to disable cache when testing
		// NoCache:    true,
	}

	// Add build args from params.Source.
	if len(params.Source.BuildArgs) > 0 {
		for k, v := range params.Source.BuildArgs {
			value := v
			buildOpts.BuildArgs[k] = &value
		}
	}

	// Add environment variables to build args to make them available during build.
	if len(params.EnvVars) > 0 {
		decryptedEnvVars, err := config.DecryptEnvVars(params.EnvVars)
		if err != nil {
			return fmt.Errorf("failed to decrypt environment variables: %w", err)
		}
		for _, envVar := range decryptedEnvVars {
			strValue, err := envVar.GetValue()
			if err != nil {
				return fmt.Errorf("failed to get value for environment variable '%s': %w", envVar.Name, err)
			}
			value := strValue
			buildOpts.BuildArgs[envVar.Name] = &value
		}
	}

	// Get ignore patterns from the original build context.
	ignorePatterns := getDockerIgnorePatterns(absContext)

	var buildContextTar io.ReadCloser
	var cleanupFunc func()

	if isDockerfileInContext {
		buildContextTar, err = archive.TarWithOptions(absContext, &archive.TarOptions{
			ExcludePatterns: ignorePatterns,
		})
		if err != nil {
			return fmt.Errorf("failed to create build context archive from '%s': %w", absContext, err)
		}
	} else {
		tmpDir, err := os.MkdirTemp("", "haloy-docker-build-")
		if err != nil {
			return fmt.Errorf("failed to create temporary build context directory: %w", err)
		}
		cleanupFunc = func() { _ = os.RemoveAll(tmpDir) }

		if err := copyDir(absContext, tmpDir); err != nil {
			cleanupFunc()
			return fmt.Errorf("failed to copy build context from '%s' to '%s': %w", absContext, tmpDir, err)
		}

		dockerfileBaseName := filepath.Base(absDockerfile)
		tmpDockerfilePath := filepath.Join(tmpDir, dockerfileBaseName)
		if err := copyFile(absDockerfile, tmpDockerfilePath); err != nil {
			cleanupFunc()
			return fmt.Errorf("failed to copy Dockerfile from '%s' to '%s': %w", absDockerfile, tmpDockerfilePath, err)
		}

		buildContextTar, err = archive.TarWithOptions(tmpDir, &archive.TarOptions{
			ExcludePatterns: ignorePatterns,
		})
		if err != nil {
			cleanupFunc()
			return fmt.Errorf("failed to create temporary build context archive from '%s': %w", tmpDir, err)
		}
	}
	defer func() {
		if buildContextTar != nil {
			buildContextTar.Close()
		}
		if cleanupFunc != nil {
			cleanupFunc()
		}
	}()

	// Check if image already exists (suggesting cache might be available)
	_, err = params.DockerClient.ImageInspect(params.Context, params.ImageName)
	cacheExists := err == nil

	startMsg := "Starting Docker build..."
	if !cacheExists {
		startMsg += " (this may take a while for first build)"
	}
	ui.Info("%s\n", startMsg)

	// Periodic progress messages.
	done := make(chan bool)
	go func() {
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				ui.Info("Build still in progress...\n")
			case <-done:
				return
			}
		}
	}()

	resp, err := params.DockerClient.ImageBuild(params.Context, buildContextTar, buildOpts)
	if err != nil {
		if errors.Is(params.Context.Err(), context.Canceled) {
			return fmt.Errorf("image build cancelled: %w", params.Context.Err())
		}
		if errors.Is(params.Context.Err(), context.DeadlineExceeded) {
			return fmt.Errorf("image build timed out: %w", params.Context.Err())
		}
		return fmt.Errorf("failed to initiate image build for '%s': %w", params.ImageName, err)
	}
	defer resp.Body.Close()

	decoder := json.NewDecoder(resp.Body)
	var lastError error

	for {
		var jsonMessage jsonmessage.JSONMessage
		if err := decoder.Decode(&jsonMessage); err != nil {
			if err == io.EOF {
				break // End of stream
			}
			close(done) // Close on decode error
			return fmt.Errorf("failed to decode docker build output: %w", err)
		}

		// Process build output
		if jsonMessage.Stream != "" {
			fmt.Print(jsonMessage.Stream)
		} else if jsonMessage.Status != "" {
			// Only print status lines that aren't download/extract progress
			if !strings.Contains(jsonMessage.Status, "Downloading") &&
				!strings.Contains(jsonMessage.Status, "Extracting") {
				fmt.Printf("%s\n", jsonMessage.Status)
			}
		} else if jsonMessage.ErrorMessage != "" {
			lastError = fmt.Errorf("%s", jsonMessage.ErrorMessage)
			fmt.Fprintf(os.Stderr, "ERROR: %s\n", jsonMessage.ErrorMessage)
		}
	}

	// If there was an error in the build, return it
	if lastError != nil {
		close(done) // Close on build error
		return fmt.Errorf("build failed: %w", lastError)
	}
	close(done)
	ui.Info("Build completed!\n")
	return nil
}

// getDockerIgnorePatterns reads the .dockerignore file and returns a slice of patterns.
func getDockerIgnorePatterns(contextDir string) []string {
	dockerIgnorePath := filepath.Join(contextDir, ".dockerignore")
	patterns := []string{}
	data, err := os.ReadFile(dockerIgnorePath)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			ui.Warning("Error reading .dockerignore file at '%s': %v", dockerIgnorePath, err)
		}
		return patterns
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "#") {
			patterns = append(patterns, line)
		}
	}
	return patterns
}

// copyDir recursively copies the directory tree from src to dst.
func copyDir(src, dst string) error {
	srcInfo, err := os.Stat(src)
	if err != nil {
		return fmt.Errorf("failed to stat source directory '%s': %w", src, err)
	}
	if !srcInfo.IsDir() {
		return fmt.Errorf("source '%s' is not a directory", src)
	}
	if err := os.MkdirAll(dst, srcInfo.Mode()); err != nil {
		return fmt.Errorf("failed to create destination directory '%s': %w", dst, err)
	}
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return fmt.Errorf("error walking directory at '%s': %w", path, err)
		}
		relPath, err := filepath.Rel(src, path)
		if err != nil {
			return fmt.Errorf("failed to calculate relative path for '%s' from base '%s': %w", path, src, err)
		}
		targetPath := filepath.Join(dst, relPath)
		if d.IsDir() {
			info, statErr := os.Stat(path)
			if statErr != nil {
				return fmt.Errorf("failed to stat source directory '%s': %w", path, statErr)
			}
			if err := os.MkdirAll(targetPath, info.Mode()); err != nil {
				return fmt.Errorf("failed to create directory '%s': %w", targetPath, err)
			}
		} else {
			if err := copyFile(path, targetPath); err != nil {
				return err
			}
		}
		return nil
	})
}

// copyFile copies a single file from src to dst, preserving permissions.
func copyFile(src, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("failed to open source file '%s': %w", src, err)
	}
	defer srcFile.Close()
	info, err := os.Stat(src)
	if err != nil {
		return fmt.Errorf("failed to stat source file '%s': %w", src, err)
	}
	dstFile, err := os.Create(dst)
	if err != nil {
		return fmt.Errorf("failed to create destination file '%s': %w", dst, err)
	}
	_, err = io.Copy(dstFile, srcFile)
	if err != nil {
		dstFile.Close()
		return fmt.Errorf("failed to copy content from '%s' to '%s': %w", src, dst, err)
	}
	if err := dstFile.Close(); err != nil {
		return fmt.Errorf("failed to close destination file '%s': %w", dst, err)
	}
	if err := os.Chmod(dst, info.Mode()); err != nil {
		ui.Warning("Failed to set permissions on '%s' (mode %s): %v", dst, info.Mode(), err)
		return fmt.Errorf("failed to set permissions on '%s': %w", dst, err)
	}
	return nil
}
