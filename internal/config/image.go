package config

import (
	"fmt"
	"os"
	"strings"

	"github.com/ameistad/haloy/internal/db"
	"github.com/docker/docker/api/types/registry"
)

type Image struct {
	// Repository should include registry if not Docker Hub, e.g. "ghcr.io/myorg/myapp"
	Repository   string        `yaml:"repository" json:"repository" toml:"repository" mapstructure:"repository"`
	Tag          string        `yaml:"tag,omitempty" json:"tag,omitempty" toml:"tag,omitempty" mapstructure:"tag,omitempty"`
	RegistryAuth *RegistryAuth `yaml:"registry,omitempty" json:"registry,omitempty" toml:"registry,omitempty" mapstructure:"registry,omitempty"`
}

type RegistryAuth struct {
	// Server is optional. If not set, it will be parsed from the Repository field.
	Server   string             `yaml:"server,omitempty" json:"server,omitempty" toml:"server,omitempty" mapstructure:"server,omitempty"`
	Username RegistryAuthSource `yaml:"username" json:"username" toml:"username" mapstructure:"username"`
	Password RegistryAuthSource `yaml:"password" json:"password" toml:"password" mapstructure:"password"`
}

type RegistryAuthSource struct {
	Type  string `yaml:"type" json:"type" toml:"type" mapstructure:"type"`     // "env", "secret", or "plain"
	Value string `yaml:"value" json:"value" toml:"value" mapstructure:"value"` // env var name, secret name, or plain value
}

func (is *Image) ImageRef() string {
	repo := strings.TrimSpace(is.Repository)
	tag := strings.TrimSpace(is.Tag)
	if tag == "" {
		tag = "latest"
	}
	return fmt.Sprintf("%s:%s", repo, tag)
}

func (is *Image) RegistryAuthString() (string, error) {
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
		database, err := db.New()
		if err != nil {
			return "", fmt.Errorf("failed to create secrets manager: %w", err)
		}
		defer database.Close()
		decrypted, err := database.GetSecretDecryptedValue(ras.Value)
		if err != nil {
			return "", fmt.Errorf("failed to get secret '%s': %w", ras.Value, err)
		}
		if decrypted == "" {
			return "", fmt.Errorf("secret '%s' is empty", ras.Value)
		}
		return decrypted, nil
	case "plain":
		return ras.Value, nil
	default:
		return "", fmt.Errorf("unsupported registry auth type: %s", ras.Type)
	}
}

func (i *Image) Validate() error {
	if strings.TrimSpace(i.Repository) == "" {
		return fmt.Errorf("source.image.repository is required")
	}
	if strings.ContainsAny(i.Repository, " \t\n\r") {
		return fmt.Errorf("source.image.repository '%s' contains whitespace", i.Repository)
	}
	if strings.ContainsAny(i.Tag, " \t\n\r") {
		return fmt.Errorf("source.image.tag '%s' contains whitespace", i.Tag)
	}

	// Validate RegistryAuth if present
	if i.RegistryAuth != nil {
		reg := i.RegistryAuth
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
