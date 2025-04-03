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

// package docker

// import (
// 	"context"
// 	"encoding/json"
// 	"errors"
// 	"fmt"
// 	"io"
// 	"io/fs"
// 	"os"
// 	"path/filepath"
// 	"strings"
// 	"time"

// 	"github.com/ameistad/haloy/internal/config"
// 	"github.com/ameistad/haloy/internal/ui"

// 	"github.com/docker/docker/api/types"
// 	"github.com/docker/docker/client"
// 	"github.com/docker/docker/pkg/archive"
// 	"github.com/docker/docker/pkg/jsonmessage"
// )

// func BuildImage(ctx context.Context, dockerClient *client.Client, imageName string, source *config.DockerfileSource) error {
// 	// Get absolute paths first to correctly determine relative paths later
// 	absContext, err := filepath.Abs(params.Source.BuildContext)
// 	if err != nil {
// 		return fmt.Errorf("failed to resolve absolute path for build context '%s': %w", params.Source.BuildContext, err)
// 	}
// 	absDockerfile, err := filepath.Abs(params.Source.Path)
// 	if err != nil {
// 		return fmt.Errorf("failed to resolve absolute path for Dockerfile '%s': %w", params.Source.Path, err)
// 	}

// 	// Check if the resolved Dockerfile path is within the resolved build context path
// 	isDockerfileInContext := strings.HasPrefix(absDockerfile, absContext+string(filepath.Separator)) || absDockerfile == absContext

// 	// Calculate the correct Dockerfile path to use in Docker API
// 	var dockerfilePath string
// 	if isDockerfileInContext {
// 		// If Dockerfile is inside context, use path relative to context
// 		relPath, err := filepath.Rel(absContext, absDockerfile)
// 		if err != nil {
// 			return fmt.Errorf("failed to calculate relative Dockerfile path: %w", err)
// 		}
// 		dockerfilePath = relPath
// 	} else {
// 		// If Dockerfile is outside context, we'll place it at root of temp context
// 		dockerfilePath = filepath.Base(absDockerfile)
// 	}

// 	buildOpts := types.ImageBuildOptions{
// 		Tags:       []string{imageName},
// 		Dockerfile: dockerfilePath,
// 		BuildArgs:  make(map[string]*string),
// 		Remove:     true, // Remove intermediate containers after build
// 		Version:    types.BuilderBuildKit,
// 		// Add this line to disable cache when testing
// 		NoCache: true,
// 	}

// 	// Set build args from the source
// 	if len(params.Source.BuildArgs) > 0 {
// 		for k, v := range params.Source.BuildArgs {
// 			value := v // Create new variable for pointer capture in loop
// 			buildOpts.BuildArgs[k] = &value
// 		}
// 	}

// 	// Get ignore patterns from the *original* build context directory, regardless of where Dockerfile is.
// 	ignorePatterns := getDockerIgnorePatterns(absContext)

// 	var buildContextTar io.ReadCloser // This will be the tar stream sent to the daemon
// 	var cleanupFunc func()            // Function to clean up temp resources if needed

// 	if isDockerfileInContext {
// 		// Case 1: Dockerfile is inside the build context directory.
// 		buildContextTar, err = archive.TarWithOptions(absContext, &archive.TarOptions{
// 			// Compression: archive.Gzip, // Optional: Can add compression
// 			ExcludePatterns: ignorePatterns,
// 		})
// 		if err != nil {
// 			return fmt.Errorf("failed to create build context archive from '%s': %w", absContext, err)
// 		}
// 	} else {
// 		// Case 2: Dockerfile is outside the context directory.
// 		tmpDir, err := os.MkdirTemp("", "haloy-docker-build-")
// 		if err != nil {
// 			return fmt.Errorf("failed to create temporary build context directory: %w", err)
// 		}

// 		// Assign the cleanup function to remove the temp dir later
// 		cleanupFunc = func() {
// 			_ = os.RemoveAll(tmpDir) // Use RemoveAll and ignore error on cleanup
// 		}

// 		// Copy the original build context content to the temp directory
// 		if err := copyDir(absContext, tmpDir); err != nil {
// 			cleanupFunc() // Attempt cleanup immediately on error
// 			return fmt.Errorf("failed to copy build context from '%s' to '%s': %w", absContext, tmpDir, err)
// 		}

// 		// Copy the external Dockerfile into the root of the temp directory
// 		dockerfileBaseName := filepath.Base(absDockerfile)
// 		tmpDockerfilePath := filepath.Join(tmpDir, dockerfileBaseName)
// 		if err := copyFile(absDockerfile, tmpDockerfilePath); err != nil {
// 			cleanupFunc() // Attempt cleanup immediately on error
// 			return fmt.Errorf("failed to copy Dockerfile from '%s' to '%s': %w", absDockerfile, tmpDockerfilePath, err)
// 		}

// 		// Archive the *temporary* directory, applying the ignore patterns
// 		// that were read from the *original* build context.
// 		buildContextTar, err = archive.TarWithOptions(tmpDir, &archive.TarOptions{
// 			// Compression: archive.Gzip, // Optional
// 			ExcludePatterns: ignorePatterns, // Apply original ignore patterns to temp dir
// 		})
// 		if err != nil {
// 			cleanupFunc() // Attempt cleanup immediately on error
// 			return fmt.Errorf("failed to create temporary build context archive from '%s': %w", tmpDir, err)
// 		}
// 	}

// 	// Ensure deferred functions run: close tar stream, run cleanup if needed.
// 	// This structure ensures Close() happens before RemoveAll().
// 	defer func() {
// 		if buildContextTar != nil {
// 			buildContextTar.Close()
// 		}
// 		if cleanupFunc != nil {
// 			cleanupFunc()
// 		}
// 	}()

// 	ui.Info("Starting Docker build... (this may take a while)\n")
// 	// Add a goroutine to show periodic messages while waiting
// 	done := make(chan bool)
// 	go func() {
// 		ticker := time.NewTicker(15 * time.Second)
// 		defer ticker.Stop()

// 		for {
// 			select {
// 			case <-ticker.C:
// 				ui.Info("Build still in progress...\n")
// 			case <-done:
// 				return
// 			}
// 		}
// 	}()
// 	resp, err := dockerClient.ImageBuild(ctx, buildContextTar, buildOpts)
// 	if err != nil {
// 		// Check for context cancellation or deadline first
// 		if errors.Is(ctx.Err(), context.Canceled) {
// 			return fmt.Errorf("image build cancelled: %w", ctx.Err())
// 		}
// 		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
// 			return fmt.Errorf("image build timed out: %w", ctx.Err())
// 		}
// 		// Otherwise, it's likely an API error initiating the build
// 		return fmt.Errorf("failed to initiate image build for '%s': %w", imageName, err)
// 	}
// 	// Must close the response body
// 	defer resp.Body.Close()

// 	// --- Stream Output ---
// 	decoder := json.NewDecoder(resp.Body)
// 	var lastError *jsonmessage.JSONError

// 	for {
// 		var jsonMessage jsonmessage.JSONMessage
// 		if err := decoder.Decode(&jsonMessage); err != nil {
// 			if err == io.EOF {
// 				break // End of stream
// 			}
// 			// Handle potential decoding errors (e.g., invalid JSON)
// 			// It might be better to log this and continue if possible,
// 			// or return a more generic error.
// 			ui.Warning("Error decoding JSON message from Docker daemon: %v\n", err)
// 			// Depending on the error, you might want to break or return
// 			// If it's just one bad message, maybe continue?
// 			// For now, let's return an error to be safe.
// 			return fmt.Errorf("failed to decode docker build output: %w", err)
// 		}

// 		// Print the stream content directly
// 		if jsonMessage.Stream != "" {
// 			// Use ui.Info or just fmt.Print depending on your UI needs
// 			// Using os.Stdout directly matches DisplayJSONMessagesStream's target
// 			fmt.Fprint(os.Stdout, jsonMessage.Stream)
// 		} else if jsonMessage.Status != "" {
// 			// Optionally print status updates (e.g., "Downloading [====>]")
// 			// You might want more sophisticated handling based on ID and Progress
// 			// fmt.Fprintf(os.Stdout, "Status: %s %s %s\n", jsonMessage.Status, jsonMessage.ID, jsonMessage.Progress)
// 			// For simplicity, maybe just print status lines that aren't downloads/extractions
// 			if !strings.Contains(jsonMessage.Status, "Downloading") && !strings.Contains(jsonMessage.Status, "Extracting") {
// 				fmt.Fprintf(os.Stdout, "%s\n", jsonMessage.Status)
// 			}

// 		} else if jsonMessage.ErrorMessage != "" {
// 			// Keep track of the last error message from the stream
// 			lastError = jsonMessage.Error
// 			fmt.Fprintf(os.Stderr, "ERROR: %s\n", jsonMessage.ErrorMessage) // Print error to stderr
// 		}

// 		// You could add more handling here for jsonMessage.Progress if needed
// 	}

// 	// After processing the stream, check if there was an error message embedded
// 	if lastError != nil {
// 		return fmt.Errorf("build failed with error from Docker daemon: %s", lastError.Message)
// 	}
// 	// termFd, isTerm := term.GetFdInfo(os.Stdout)
// 	// err = jsonmessage.DisplayJSONMessagesStream(resp.Body, os.Stdout, termFd, isTerm, nil)
// 	// if err != nil {
// 	// 	if jsonErr, ok := err.(*jsonmessage.JSONError); ok {
// 	// 		return fmt.Errorf("build failed with error from Docker daemon: %s", jsonErr.Message)
// 	// 	}
// 	// 	return fmt.Errorf("failed to stream build output: %w", err)
// 	// }

// 	ui.Info("Build completed!\n")
// 	return nil
// }

// // getDockerIgnorePatterns reads the .dockerignore file in the given context directory
// // and returns a slice of patterns. Returns empty slice if file doesn't exist or on error.
// func getDockerIgnorePatterns(contextDir string) []string {
// 	dockerIgnorePath := filepath.Join(contextDir, ".dockerignore")
// 	patterns := []string{}
// 	data, err := os.ReadFile(dockerIgnorePath)
// 	if err != nil {
// 		if !errors.Is(err, fs.ErrNotExist) {
// 			// Log unexpected errors reading .dockerignore, but don't fail the build
// 			ui.Warning("Error reading .dockerignore file at '%s': %v", dockerIgnorePath, err)
// 		}
// 		// File doesn't exist or couldn't be read; return empty slice (no ignore patterns).
// 		return patterns
// 	}

// 	for _, line := range strings.Split(string(data), "\n") {
// 		line = strings.TrimSpace(line)
// 		// Ignore empty lines and comments
// 		if line != "" && !strings.HasPrefix(line, "#") {
// 			patterns = append(patterns, line)
// 		}
// 	}
// 	return patterns
// }

// // copyDir recursively copies the directory tree from src to dst.
// func copyDir(src string, dst string) error {
// 	srcInfo, err := os.Stat(src)
// 	if err != nil {
// 		return fmt.Errorf("failed to stat source directory '%s': %w", src, err)
// 	}
// 	if !srcInfo.IsDir() {
// 		return fmt.Errorf("source '%s' is not a directory", src)
// 	}

// 	// Create the destination directory with the same permissions as the params.Source.
// 	if err := os.MkdirAll(dst, srcInfo.Mode()); err != nil {
// 		return fmt.Errorf("failed to create destination directory '%s': %w", dst, err)
// 	}

// 	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
// 		if err != nil {
// 			// Handle potential errors from WalkDir itself (e.g., permission issues)
// 			return fmt.Errorf("error walking directory at '%s': %w", path, err)
// 		}

// 		// Calculate the relative path from the source base to the current item
// 		relPath, err := filepath.Rel(src, path)
// 		if err != nil {
// 			// This error should ideally not happen if path is within src
// 			return fmt.Errorf("failed to calculate relative path for '%s' from base '%s': %w", path, src, err)
// 		}

// 		// Determine the full path in the destination directory
// 		targetPath := filepath.Join(dst, relPath)

// 		if d.IsDir() {
// 			// If it's a directory, create it in the destination.
// 			// MkdirAll handles cases where the directory already exists.
// 			// Stat the original directory to get its permissions.
// 			info, statErr := os.Stat(path)
// 			if statErr != nil {
// 				return fmt.Errorf("failed to stat source directory '%s' for permissions: %w", path, statErr)
// 			}
// 			if err := os.MkdirAll(targetPath, info.Mode()); err != nil {
// 				return fmt.Errorf("failed to create directory '%s': %w", targetPath, err)
// 			}
// 		} else {
// 			// If it's a file, copy it.
// 			if err := copyFile(path, targetPath); err != nil {
// 				// Error is already wrapped in copyFile
// 				return err
// 			}
// 		}
// 		return nil
// 	})
// }

// // copyFile copies a single file from src to dst, preserving permissions.
// func copyFile(src, dst string) error {
// 	srcFile, err := os.Open(src)
// 	if err != nil {
// 		return fmt.Errorf("failed to open source file '%s': %w", src, err)
// 	}
// 	defer srcFile.Close()

// 	// Get permissions from source file *before* creating destination
// 	info, err := os.Stat(src)
// 	if err != nil {
// 		return fmt.Errorf("failed to stat source file '%s' for permissions: %w", src, err)
// 	}

// 	// Create destination file. Use Create for simplicity (truncates if exists).
// 	// Consider O_WRONLY|O_CREATE|O_TRUNC if more control is needed.
// 	dstFile, err := os.Create(dst)
// 	if err != nil {
// 		return fmt.Errorf("failed to create destination file '%s': %w", dst, err)
// 	}
// 	// Defer closing the destination file *before* Chmod.
// 	defer dstFile.Close()

// 	// Copy content
// 	_, err = io.Copy(dstFile, srcFile)
// 	if err != nil {
// 		return fmt.Errorf("failed to copy content from '%s' to '%s': %w", src, dst, err)
// 	}

// 	// Explicitly close destination file before Chmod, as some OS require this.
// 	err = dstFile.Close()
// 	if err != nil {
// 		return fmt.Errorf("failed to close destination file '%s' before chmod: %w", dst, err)
// 	}

// 	// Apply source permissions to destination file
// 	err = os.Chmod(dst, info.Mode())
// 	if err != nil {
// 		// Log failure to set permissions but maybe don't fail the whole copy? Depends on requirements.
// 		// For a build context, permissions usually matter.
// 		ui.Warning("Failed to set permissions on destination file '%s' (mode %s): %v", dst, info.Mode(), err)
// 		// Return error to be safe, as permissions can affect build reproducibility/outcome.
// 		return fmt.Errorf("failed to set permissions on destination file '%s': %w", dst, err)
// 	}

// 	return nil
// }
