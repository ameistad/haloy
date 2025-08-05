package config

import (
	"fmt"
	"strings"
)

type Image struct {
	// Repository should include registry if not Docker Hub, e.g. "ghcr.io/myorg/myapp"
	Repository   string        `yaml:"repository" json:"repository" toml:"repository"`
	Tag          string        `yaml:"tag,omitempty" json:"tag,omitempty" toml:"tag,omitempty"`
	RegistryAuth *RegistryAuth `yaml:"registry,omitempty" json:"registry,omitempty" toml:"registry,omitempty"`
}

type RegistryAuth struct {
	// Server is optional. If not set, it will be parsed from the Repository field.
	Server   string             `yaml:"server,omitempty" json:"server,omitempty" toml:"server,omitempty"`
	Username RegistryAuthSource `yaml:"username" json:"username" toml:"username"`
	Password RegistryAuthSource `yaml:"password" json:"password" toml:"password"`
}

type RegistryAuthSource struct {
	Type  string `yaml:"type" json:"type" toml:"type"`    // "env", "secret", or "plain"
	Value string `yaml:"value" json:"value" toml:"value"` // env var name, secret name, or plain value
}

func (is *Image) ImageRef() string {
	repo := strings.TrimSpace(is.Repository)
	tag := strings.TrimSpace(is.Tag)
	if tag == "" {
		tag = "latest"
	}
	return fmt.Sprintf("%s:%s", repo, tag)
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
