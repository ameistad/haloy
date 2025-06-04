package config

import (
	"fmt"
	"os"
	"reflect"
	"strings"

	"github.com/docker/docker/api/types/registry"
	"gopkg.in/yaml.v3"
)

type Source struct {
	// Use pointers to ensure only one is provided.
	Dockerfile *DockerfileSource `yaml:"dockerfile,omitempty"`
	Image      *ImageSource      `yaml:"image,omitempty"`
}

type DockerfileSource struct {
	Path         string            `yaml:"path"`
	BuildContext string            `yaml:"buildContext"`
	BuildArgs    map[string]string `yaml:"buildArgs,omitempty"`
}

type ImageSource struct {
	// Repository should include registry if not Docker Hub, e.g. "ghcr.io/myorg/myapp"
	Repository   string        `yaml:"repository"`
	Tag          string        `yaml:"tag,omitempty"`
	RegistryAuth *RegistryAuth `yaml:"registry,omitempty"`
}

type RegistryAuth struct {
	// Server is optional. If not set, it will be parsed from the Repository field.
	Server   string             `yaml:"server,omitempty"`
	Username RegistryAuthSource `yaml:"username"`
	Password RegistryAuthSource `yaml:"password"`
}

type RegistryAuthSource struct {
	Type  string `yaml:"type"`  // "env", "secret", or "plain"
	Value string `yaml:"value"` // env var name, secret name, or plain value
}

func (is *ImageSource) ImageRef() string {
	repo := strings.TrimSpace(is.Repository)
	tag := strings.TrimSpace(is.Tag)
	if tag == "" {
		tag = "latest"
	}
	return fmt.Sprintf("%s:%s", repo, tag)
}

func (is *ImageSource) RegistryAuthString() (string, error) {
	if is.RegistryAuth == nil {
		return "", nil
	}
	username, err := resolveRegistryAuthSource(is.RegistryAuth.Username)
	if err != nil {
		return "", err
	}
	password, err := resolveRegistryAuthSource(is.RegistryAuth.Password)
	if err != nil {
		return "", err
	}
	server := "index.docker.io" // Default to Docker Hub if no server specified
	if is.RegistryAuth.Server != "" {
		server = is.RegistryAuth.Server
	} else {
		// If no server is set, parse it from the Repository field
		parts := strings.SplitN(is.Repository, "/", 2)
		if len(parts) > 1 && strings.Contains(parts[0], ".") {
			server = parts[0]
		}
	}
	authConfig := registry.AuthConfig{
		Username:      username,
		Password:      password,
		ServerAddress: server,
	}
	authStr, err := registry.EncodeAuthConfig(authConfig)
	if err != nil {
		return "", err
	}
	return authStr, nil
}

func resolveRegistryAuthSource(ras RegistryAuthSource) (string, error) {
	switch ras.Type {
	case "env":
		return os.Getenv(ras.Value), nil
	case "secret":
		secrets, err := LoadSecrets()
		if err != nil {
			return "", fmt.Errorf("failed to load secrets: %w", err)
		}

		identity, err := GetAgeIdentity()
		if err != nil {
			return "", fmt.Errorf("failed to get age identity: %w", err)
		}
		record, exists := secrets[ras.Value]
		if !exists {
			return "", fmt.Errorf("secret '%s' not found in secrets", ras.Value)
		}

		decrypted, err := DecryptSecret(record.Encrypted, identity)
		if err != nil {
			return "", fmt.Errorf("failed to decrypt secret '%s': %w", ras.Value, err)
		}
		return decrypted, nil
	case "plain":
		return ras.Value, nil
	default:
		return "", fmt.Errorf("unsupported registry auth type: %s", ras.Type)
	}
}

func (s *Source) UnmarshalYAML(value *yaml.Node) error {
	// Get expected field names
	expectedFields := ExtractYAMLFieldNames(reflect.TypeOf(*s))

	// Check for unknown fields
	if err := CheckUnknownFields(value, expectedFields, "source: "); err != nil {
		return err
	}

	// Use type alias to avoid infinite recursion
	type SourceAlias Source
	var alias SourceAlias

	// Unmarshal to the alias type
	if err := value.Decode(&alias); err != nil {
		return err
	}

	// Copy data back to original struct
	*s = Source(alias)

	return nil
}

func (s *Source) Validate() error {
	sourceIsDefined := false

	// Check Dockerfile Source
	if s.Dockerfile != nil {
		sourceIsDefined = true
		dfSource := s.Dockerfile
		if dfSource.Path == "" {
			return fmt.Errorf("source.dockerfile.path is required")
		}
		if dfSource.BuildContext == "" {
			return fmt.Errorf("source.dockerfile.buildContext is required")
		}

		// Check Dockerfile Path existence and type (should be a file)
		// Consider making paths absolute before checking, or resolving relative to config file?
		// For now, assuming paths are relative to where the app runs or absolute.
		fileInfo, err := os.Stat(dfSource.Path)
		if os.IsNotExist(err) {
			return fmt.Errorf("source.dockerfile.path '%s' does not exist", dfSource.Path)
		} else if err != nil {
			return fmt.Errorf("unable to check source.dockerfile.path '%s': %w", dfSource.Path, err)
		}
		if fileInfo.IsDir() {
			return fmt.Errorf("source.dockerfile.path '%s' is a directory, not a file", dfSource.Path)
		}

		// Check BuildContext existence and type (should be a directory)
		ctxInfo, err := os.Stat(dfSource.BuildContext)
		if os.IsNotExist(err) {
			return fmt.Errorf("source.dockerfile.buildContext '%s' does not exist", dfSource.BuildContext)
		} else if err != nil {
			return fmt.Errorf("unable to check source.dockerfile.buildContext '%s': %w", dfSource.BuildContext, err)
		}
		if !ctxInfo.IsDir() {
			return fmt.Errorf("source.dockerfile.buildContext '%s' is not a directory", dfSource.BuildContext)
		}
	}

	// Check Image Source
	if s.Image != nil {
		// Check if Dockerfile source was *also* defined (mutual exclusivity)
		if sourceIsDefined {
			return fmt.Errorf("cannot define both source.dockerfile and source.image")
		}
		sourceIsDefined = true
		imgSource := s.Image

		// Validate Image source fields
		if strings.TrimSpace(imgSource.Repository) == "" {
			return fmt.Errorf("source.image.repository is required")
		}
		if strings.ContainsAny(imgSource.Repository, " \t\n\r") {
			return fmt.Errorf("source.image.repository '%s' contains whitespace", imgSource.Repository)
		}
		if strings.ContainsAny(imgSource.Tag, " \t\n\r") {
			return fmt.Errorf("source.image.tag '%s' contains whitespace", imgSource.Tag)
		}

		// Validate RegistryAuth if present
		if imgSource.RegistryAuth != nil {
			reg := imgSource.RegistryAuth
			// Server is now optional; if empty, it will be parsed from Repository at runtime.
			if strings.TrimSpace(reg.Server) != "" && strings.ContainsAny(reg.Server, " \t\n\r") {
				return fmt.Errorf("source.image.registry.server '%s' contains whitespace", reg.Server)
			}
			// Validate Username
			if err := validateRegistryAuthSource("username", reg.Username); err != nil {
				return err
			}
			// Validate Password
			if err := validateRegistryAuthSource("password", reg.Password); err != nil {
				return err
			}
		}
	}

	// Check if *at least one* source type was defined
	if !sourceIsDefined {
		return fmt.Errorf("source must contain either 'dockerfile' or 'image'")
	}
	return nil
}

func validateRegistryAuthSource(field string, ras RegistryAuthSource) error {
	validTypes := map[string]bool{"env": true, "secret": true, "plain": true}
	if strings.TrimSpace(ras.Type) == "" {
		return fmt.Errorf("source.image.registry.%s.type is required", field)
	}
	if !validTypes[ras.Type] {
		return fmt.Errorf("source.image.registry.%s.type '%s' is invalid (must be 'env', 'secret', or 'plain')", field, ras.Type)
	}
	if strings.TrimSpace(ras.Value) == "" {
		return fmt.Errorf("source.image.registry.%s.value is required", field)
	}
	return nil
}
