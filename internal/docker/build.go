package docker

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/ameistad/haloy/internal/config"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/archive"
	"github.com/docker/docker/pkg/jsonmessage"
	"github.com/moby/term"
)

// BuildImage builds a Docker image mimicking the 'docker build' CLI behavior.
func BuildImage(imageName string, dockerClient *client.Client, ctx context.Context, appConfig *config.AppConfig) error {
	fmt.Printf("Building image '%s'...\n", imageName)

	// Prepare build options.
	buildOpts := types.ImageBuildOptions{
		Tags:       []string{imageName},
		Dockerfile: filepath.Base(appConfig.Dockerfile),
		BuildArgs:  make(map[string]*string),
		Remove:     true,
	}
	for k, v := range appConfig.Env {
		value := v // create a new variable for the pointer
		buildOpts.BuildArgs[k] = &value
	}

	// Determine whether the Dockerfile is within the build context.
	absContext, err := filepath.Abs(appConfig.BuildContext)
	if err != nil {
		return fmt.Errorf("failed to resolve absolute path for build context: %w", err)
	}
	absDockerfile, err := filepath.Abs(appConfig.Dockerfile)
	if err != nil {
		return fmt.Errorf("failed to resolve absolute path for Dockerfile: %w", err)
	}

	var buildContextTar io.ReadCloser
	// If the Dockerfile is inside the build context, archive the entire directory.
	if strings.HasPrefix(absDockerfile, absContext) {
		buildContextTar, err = archive.TarWithOptions(appConfig.BuildContext, &archive.TarOptions{
			ExcludePatterns: getDockerIgnorePatterns(appConfig.BuildContext),
		})
		if err != nil {
			return fmt.Errorf("failed to create build context: %w", err)
		}
		defer buildContextTar.Close()
	} else {
		// If the Dockerfile is outside the build context, merge them in a temporary directory.
		tmpDir, err := os.MkdirTemp("", "docker-build-context")
		if err != nil {
			return fmt.Errorf("failed to create temporary build context directory: %w", err)
		}
		defer os.RemoveAll(tmpDir)

		// Copy the entire build context into the temporary directory.
		if err := copyDir(appConfig.BuildContext, tmpDir); err != nil {
			return fmt.Errorf("failed to copy build context: %w", err)
		}

		// Copy the Dockerfile into the temporary directory.
		dockerfileContent, err := os.ReadFile(appConfig.Dockerfile)
		if err != nil {
			return fmt.Errorf("failed to read Dockerfile: %w", err)
		}
		dockerfilePath := filepath.Join(tmpDir, filepath.Base(appConfig.Dockerfile))
		if err := os.WriteFile(dockerfilePath, dockerfileContent, 0644); err != nil {
			return fmt.Errorf("failed to write Dockerfile to temporary build context: %w", err)
		}

		// Archive the merged temporary directory.
		buildContextTar, err = archive.TarWithOptions(tmpDir, &archive.TarOptions{
			ExcludePatterns: getDockerIgnorePatterns(tmpDir),
		})
		if err != nil {
			return fmt.Errorf("failed to create merged build context: %w", err)
		}
		defer buildContextTar.Close()
	}

	// Execute the build.
	resp, err := dockerClient.ImageBuild(ctx, buildContextTar, buildOpts)
	if err != nil {
		return fmt.Errorf("failed to build image: %w", err)
	}
	defer resp.Body.Close()

	// Stream output similar to the docker CLI.
	termFd, isTerm := term.GetFdInfo(os.Stdout)
	return jsonmessage.DisplayJSONMessagesStream(resp.Body, os.Stdout, termFd, isTerm, nil)
}

// getDockerIgnorePatterns reads the .dockerignore file in the given context directory.
func getDockerIgnorePatterns(contextDir string) []string {
	dockerIgnorePath := filepath.Join(contextDir, ".dockerignore")
	patterns := []string{}
	data, err := os.ReadFile(dockerIgnorePath)
	if err != nil {
		// .dockerignore doesn't exist; return empty slice.
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
func copyDir(src string, dst string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relPath, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		targetPath := filepath.Join(dst, relPath)
		if d.IsDir() {
			return os.MkdirAll(targetPath, 0755)
		}
		return copyFile(path, targetPath)
	})
}

// copyFile copies a single file from src to dst.
func copyFile(src, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	dstFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer dstFile.Close()

	if _, err := io.Copy(dstFile, srcFile); err != nil {
		return err
	}

	if info, err := os.Stat(src); err == nil {
		return os.Chmod(dst, info.Mode())
	}
	return nil
}

// Legacy build with docker CLI
func BuildImageDockerCLI(imageName string, appConfig *config.AppConfig) error {

	args := []string{"build", "-t", imageName, "-f", appConfig.Dockerfile}
	for k, v := range appConfig.Env {
		args = append(args, "--build-arg", fmt.Sprintf("%s=%s", k, v))
	}
	args = append(args, appConfig.BuildContext)

	cmd := exec.Command("docker", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	fmt.Printf("Building image '%s'...\n", imageName)
	return cmd.Run()
}
