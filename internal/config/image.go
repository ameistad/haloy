package config

import (
	"fmt"
	"strings"
)

type ImageSource string

const (
	ImageSourceRegistry ImageSource = "registry"
	ImageSourceLocal    ImageSource = "local"
)

type Image struct {
	Repository   string        `json:"repository" yaml:"repository" toml:"repository"`
	Source       ImageSource   `json:"source,omitempty" yaml:"source,omitempty" toml:"source,omitempty"`
	Tag          string        `json:"tag,omitempty" yaml:"tag,omitempty" toml:"tag,omitempty"`
	RegistryAuth *RegistryAuth `json:"registry,omitempty" yaml:"registry,omitempty" toml:"registry,omitempty"`
}

type RegistryAuth struct {
	// Server is optional. If not set, it will be parsed from the Repository field.
	Server   string             `json:"server,omitempty" yaml:"server,omitempty" toml:"server,omitempty"`
	Username RegistryAuthSource `json:"username" yaml:"username" toml:"username"`
	Password RegistryAuthSource `json:"password" yaml:"password" toml:"password"`
}

type RegistryAuthSource struct {
	Type  string `json:"type" yaml:"type" toml:"type"`    // "env", "secret", or "plain"
	Value string `json:"value" yaml:"value" toml:"value"` // env var name, secret name, or plain value
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

	if i.Source != "" {
		if i.Source != ImageSourceRegistry && i.Source != ImageSourceLocal {
			return fmt.Errorf("source.image.source '%s' is invalid (must be 'registry' or 'local')", i.Source)
		}
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
