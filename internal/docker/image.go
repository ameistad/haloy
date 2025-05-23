package docker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/ameistad/haloy/internal/config"
	"github.com/ameistad/haloy/internal/logging"
	"github.com/ameistad/haloy/internal/ui"
	"github.com/rs/zerolog"

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
	LogHandler   logging.LogHandlerFunc
}

func BuildImage(params BuildImageParams) error {

	if params.LogHandler == nil {
		params.LogHandler = func(level zerolog.Level, message string, appName string) {
			fmt.Print(message)
		}
	}

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
		// NoCache: true,
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
	params.LogHandler(zerolog.InfoLevel, startMsg, "")

	// Periodic progress messages.
	done := make(chan bool)
	go func() {
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				params.LogHandler(zerolog.InfoLevel, "Build still in progress...", "")
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
	params.LogHandler(zerolog.InfoLevel, "Build completed successfully!", "")
	return nil
}

// getDockerIgnorePatterns reads the .dockerignore file and returns a slice of patterns.
func getDockerIgnorePatterns(contextDir string) []string {
	dockerIgnorePath := filepath.Join(contextDir, ".dockerignore")
	patterns := []string{}
	data, err := os.ReadFile(dockerIgnorePath)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			ui.Warn("Error reading .dockerignore file at '%s': %v", dockerIgnorePath, err)
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
		ui.Warn("Failed to set permissions on '%s' (mode %s): %v", dst, info.Mode(), err)
		return fmt.Errorf("failed to set permissions on '%s': %w", dst, err)
	}
	return nil
}

type BuildImageCLIParams struct {
	Context    context.Context
	ImageName  string
	Source     *config.DockerfileSource
	EnvVars    []config.EnvVar
	LogHandler logging.LogHandlerFunc
}

func BuildImageCLI(params BuildImageCLIParams) error {
	if params.LogHandler == nil {
		params.LogHandler = func(level zerolog.Level, message string, appName string) {
			fmt.Print(message)
		}
	}

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

	params.LogHandler(zerolog.InfoLevel, fmt.Sprintf("Building image '%s' with Docker CLI: docker %s", params.ImageName, strings.Join(cmdArgs, " ")), "")

	cmd := exec.CommandContext(params.Context, "docker", cmdArgs...)
	cmd.Dir = absContext // Set the working directory for the command

	// For real-time output, you might want to use cmd.StdoutPipe and cmd.StderrPipe
	// and process them in goroutines. For simplicity, CombinedOutput is used here.
	output, err := cmd.CombinedOutput()

	if err != nil {
		// Log the output for debugging
		params.LogHandler(zerolog.ErrorLevel, fmt.Sprintf("Docker build failed. Output:\n%s", string(output)), "")

		if exitErr, ok := err.(*exec.ExitError); ok {
			return fmt.Errorf("docker build command failed with exit code %d: %w. Output: %s", exitErr.ExitCode(), err, string(output))
		}
		return fmt.Errorf("failed to execute docker build command: %w. Output: %s", err, string(output))
	}

	params.LogHandler(zerolog.InfoLevel, fmt.Sprintf("Docker build for '%s' successful.", params.ImageName), "")
	// Optionally log success output if needed:
	// params.LogHandler(zerolog.DebugLevel, fmt.Sprintf("Docker build output:\n%s", string(output)), "")

	return nil
}
