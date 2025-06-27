package config

import (
	"fmt"
	"os"
	"strings"

	"github.com/docker/docker/api/types/registry"
)

type Image struct {
	// Repository should include registry if not Docker Hub, e.g. "ghcr.io/myorg/myapp"
	Repository   string        `json:"repository"`
	Tag          string        `json:"tag,omitempty"`
	RegistryAuth *RegistryAuth `json:"registry,omitempty"`
}

type RegistryAuth struct {
	// Server is optional. If not set, it will be parsed from the Repository field.
	Server   string             `json:"server,omitempty"`
	Username RegistryAuthSource `json:"username"`
	Password RegistryAuthSource `json:"password"`
}

type RegistryAuthSource struct {
	Type  string `json:"type"`  // "env", "secret", or "plain"
	Value string `json:"value"` // env var name, secret name, or plain value
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
