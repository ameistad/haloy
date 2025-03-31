package docker

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/ameistad/haloy/internal/config"
	"github.com/ameistad/haloy/internal/ui"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/archive"
	"github.com/docker/docker/pkg/jsonmessage"
	"github.com/moby/term" // For terminal detection in output streaming
)

func BuildImage(ctx context.Context, dockerClient *client.Client, imageName string, source *config.DockerfileSource) error {
	ui.Info("Preparing to build image '%s'...", imageName)

	// TODO: Consider adding appConfig fields for NoCache, PullParent, Platform if needed
	buildOpts := types.ImageBuildOptions{
		Tags:       []string{imageName},
		Dockerfile: filepath.Base(source.Path), // Path relative to context root
		BuildArgs:  make(map[string]*string),
		Remove:     true, // Remove intermediate containers after a successful build
		// NoCache:    appConfig.NoCache,    // Example: Add if appConfig has NoCache field
		// PullParent: appConfig.PullParent, // Example: Add if appConfig has PullParent field
		// Platform:   appConfig.Platform,   // Example: Add if appConfig has Platform field (e.g., "linux/amd64")
	}

	// Set build args from the source
	if len(source.BuildArgs) > 0 {
		for k, v := range source.BuildArgs {
			value := v // Create new variable for pointer capture in loop
			buildOpts.BuildArgs[k] = &value
		}
	}

	absContext, err := filepath.Abs(source.BuildContext)
	if err != nil {
		return fmt.Errorf("failed to resolve absolute path for build context '%s': %w", source.BuildContext, err)
	}
	absDockerfile, err := filepath.Abs(source.Path)
	if err != nil {
		return fmt.Errorf("failed to resolve absolute path for Dockerfile '%s': %w", source.Path, err)
	}

	// Get ignore patterns from the *original* build context directory, regardless of where Dockerfile is.
	ignorePatterns := getDockerIgnorePatterns(absContext)

	var buildContextTar io.ReadCloser // This will be the tar stream sent to the daemon
	var cleanupFunc func()            // Function to clean up temp resources if needed

	// Check if the resolved Dockerfile path is within the resolved build context path.
	// Need to handle separators correctly, especially if context is "/path/to/context" and dockerfile is "/path/to/context/Dockerfile".
	// Also handle the case where the context *is* the Dockerfile (less common).
	isDockerfileInContext := strings.HasPrefix(absDockerfile, absContext+string(filepath.Separator)) || absDockerfile == absContext

	if isDockerfileInContext {
		// Case 1: Dockerfile is inside the build context directory.
		ui.Info("Dockerfile found within build context. Archiving context: %s", absContext)
		buildContextTar, err = archive.TarWithOptions(absContext, &archive.TarOptions{
			// Compression: archive.Gzip, // Optional: Can add compression
			ExcludePatterns: ignorePatterns,
		})
		if err != nil {
			return fmt.Errorf("failed to create build context archive from '%s': %w", absContext, err)
		}
	} else {
		// Case 2: Dockerfile is outside the context directory.
		// We need to create a temporary directory, copy the original context into it,
		// copy the external Dockerfile into the root of the temp dir, and then archive the temp dir.
		// This is necessary because the Docker API's ImageBuild expects the Dockerfile path
		// (in ImageBuildOptions) to be relative to the root of the *streamed* build context tarball.
		ui.Warning("Dockerfile '%s' is outside build context '%s'. Creating temporary merged context.", absDockerfile, absContext)

		tmpDir, err := os.MkdirTemp("", "haloy-docker-build-")
		if err != nil {
			return fmt.Errorf("failed to create temporary build context directory: %w", err)
		}

		// Assign the cleanup function to remove the temp dir later
		cleanupFunc = func() {
			_ = os.RemoveAll(tmpDir) // Use RemoveAll and ignore error on cleanup
		}

		// Copy the original build context content to the temp directory
		if err := copyDir(absContext, tmpDir); err != nil {
			cleanupFunc() // Attempt cleanup immediately on error
			return fmt.Errorf("failed to copy build context from '%s' to '%s': %w", absContext, tmpDir, err)
		}

		// Copy the external Dockerfile into the root of the temp directory
		dockerfileBaseName := filepath.Base(absDockerfile)
		tmpDockerfilePath := filepath.Join(tmpDir, dockerfileBaseName)
		if err := copyFile(absDockerfile, tmpDockerfilePath); err != nil {
			cleanupFunc() // Attempt cleanup immediately on error
			return fmt.Errorf("failed to copy Dockerfile from '%s' to '%s': %w", absDockerfile, tmpDockerfilePath, err)
		}

		// Crucially, update buildOpts.Dockerfile to point to the basename
		// within the *temporary* context directory.
		buildOpts.Dockerfile = dockerfileBaseName

		// Archive the *temporary* directory, applying the ignore patterns
		// that were read from the *original* build context.
		buildContextTar, err = archive.TarWithOptions(tmpDir, &archive.TarOptions{
			// Compression: archive.Gzip, // Optional
			ExcludePatterns: ignorePatterns, // Apply original ignore patterns to temp dir
		})
		if err != nil {
			cleanupFunc() // Attempt cleanup immediately on error
			return fmt.Errorf("failed to create temporary build context archive from '%s': %w", tmpDir, err)
		}
	}

	// Ensure deferred functions run: close tar stream, run cleanup if needed.
	// This structure ensures Close() happens before RemoveAll().
	defer func() {
		if buildContextTar != nil {
			buildContextTar.Close()
		}
		if cleanupFunc != nil {
			cleanupFunc()
		}
	}()

	// --- Execute Build ---
	ui.Info("Starting image build for '%s' via Docker API...", imageName)
	resp, err := dockerClient.ImageBuild(ctx, buildContextTar, buildOpts)
	if err != nil {
		// Check for context cancellation or deadline first
		if errors.Is(ctx.Err(), context.Canceled) {
			return fmt.Errorf("image build cancelled: %w", ctx.Err())
		}
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return fmt.Errorf("image build timed out: %w", ctx.Err())
		}
		// Otherwise, it's likely an API error initiating the build
		return fmt.Errorf("failed to initiate image build for '%s': %w", imageName, err)
	}
	// Must close the response body
	defer resp.Body.Close()

	// --- Stream Output ---
	// Use DisplayJSONMessagesStream for nice CLI-like output to Stdout.
	termFd, isTerm := term.GetFdInfo(os.Stdout)
	err = jsonmessage.DisplayJSONMessagesStream(resp.Body, os.Stdout, termFd, isTerm, nil)
	if err != nil {
		// DisplayJSONMessagesStream returns an error if the stream contains an error message from the daemon
		// or if there's an issue reading/parsing the stream itself.
		if jsonErr, ok := err.(*jsonmessage.JSONError); ok {
			// This was an error message reported by the Docker daemon during the build.
			return fmt.Errorf("build failed with error from Docker daemon: %s", jsonErr.Message)
		}
		// This was likely an issue reading or parsing the stream.
		return fmt.Errorf("failed to stream build output: %w", err)
	}

	ui.Success("Successfully built image '%s'", imageName)
	return nil
}

// getDockerIgnorePatterns reads the .dockerignore file in the given context directory
// and returns a slice of patterns. Returns empty slice if file doesn't exist or on error.
func getDockerIgnorePatterns(contextDir string) []string {
	dockerIgnorePath := filepath.Join(contextDir, ".dockerignore")
	patterns := []string{}
	data, err := os.ReadFile(dockerIgnorePath)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			// Log unexpected errors reading .dockerignore, but don't fail the build
			ui.Warning("Error reading .dockerignore file at '%s': %v", dockerIgnorePath, err)
		}
		// File doesn't exist or couldn't be read; return empty slice (no ignore patterns).
		return patterns
	}

	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		// Ignore empty lines and comments
		if line != "" && !strings.HasPrefix(line, "#") {
			patterns = append(patterns, line)
		}
	}
	return patterns
}

// copyDir recursively copies the directory tree from src to dst.
func copyDir(src string, dst string) error {
	srcInfo, err := os.Stat(src)
	if err != nil {
		return fmt.Errorf("failed to stat source directory '%s': %w", src, err)
	}
	if !srcInfo.IsDir() {
		return fmt.Errorf("source '%s' is not a directory", src)
	}

	// Create the destination directory with the same permissions as the source.
	if err := os.MkdirAll(dst, srcInfo.Mode()); err != nil {
		return fmt.Errorf("failed to create destination directory '%s': %w", dst, err)
	}

	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			// Handle potential errors from WalkDir itself (e.g., permission issues)
			return fmt.Errorf("error walking directory at '%s': %w", path, err)
		}

		// Calculate the relative path from the source base to the current item
		relPath, err := filepath.Rel(src, path)
		if err != nil {
			// This error should ideally not happen if path is within src
			return fmt.Errorf("failed to calculate relative path for '%s' from base '%s': %w", path, src, err)
		}

		// Determine the full path in the destination directory
		targetPath := filepath.Join(dst, relPath)

		if d.IsDir() {
			// If it's a directory, create it in the destination.
			// MkdirAll handles cases where the directory already exists.
			// Stat the original directory to get its permissions.
			info, statErr := os.Stat(path)
			if statErr != nil {
				return fmt.Errorf("failed to stat source directory '%s' for permissions: %w", path, statErr)
			}
			if err := os.MkdirAll(targetPath, info.Mode()); err != nil {
				return fmt.Errorf("failed to create directory '%s': %w", targetPath, err)
			}
		} else {
			// If it's a file, copy it.
			if err := copyFile(path, targetPath); err != nil {
				// Error is already wrapped in copyFile
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

	// Get permissions from source file *before* creating destination
	info, err := os.Stat(src)
	if err != nil {
		return fmt.Errorf("failed to stat source file '%s' for permissions: %w", src, err)
	}

	// Create destination file. Use Create for simplicity (truncates if exists).
	// Consider O_WRONLY|O_CREATE|O_TRUNC if more control is needed.
	dstFile, err := os.Create(dst)
	if err != nil {
		return fmt.Errorf("failed to create destination file '%s': %w", dst, err)
	}
	// Defer closing the destination file *before* Chmod.
	defer dstFile.Close()

	// Copy content
	_, err = io.Copy(dstFile, srcFile)
	if err != nil {
		return fmt.Errorf("failed to copy content from '%s' to '%s': %w", src, dst, err)
	}

	// Explicitly close destination file before Chmod, as some OS require this.
	err = dstFile.Close()
	if err != nil {
		return fmt.Errorf("failed to close destination file '%s' before chmod: %w", dst, err)
	}

	// Apply source permissions to destination file
	err = os.Chmod(dst, info.Mode())
	if err != nil {
		// Log failure to set permissions but maybe don't fail the whole copy? Depends on requirements.
		// For a build context, permissions usually matter.
		ui.Warning("Failed to set permissions on destination file '%s' (mode %s): %v", dst, info.Mode(), err)
		// Return error to be safe, as permissions can affect build reproducibility/outcome.
		return fmt.Errorf("failed to set permissions on destination file '%s': %w", dst, err)
	}

	return nil
}
