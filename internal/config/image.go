package config

import (
	"errors"
	"fmt"
	"slices"
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
	History      *ImageHistory `json:"history,omitempty" yaml:"history,omitempty" toml:"history,omitempty"`
	RegistryAuth *RegistryAuth `json:"registry,omitempty" yaml:"registry,omitempty" toml:"registry,omitempty"`
	Builder      *Builder      `json:"builder,omitempty" yaml:"builder,omitempty" toml:"builder,omitempty"`
}

type RegistryAuth struct {
	// Server is optional. If not set, it will be parsed from the Repository field.
	Server   string      `json:"server,omitempty" yaml:"server,omitempty" toml:"server,omitempty"`
	Username ValueSource `json:"username" yaml:"username" toml:"username"`
	Password ValueSource `json:"password" yaml:"password" toml:"password"`
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
		return fmt.Errorf("image.repository is required")
	}
	if strings.ContainsAny(i.Repository, " \t\n\r") {
		return fmt.Errorf("image.repository '%s' contains whitespace", i.Repository)
	}

	if i.Source != "" {
		if i.Source != ImageSourceRegistry && i.Source != ImageSourceLocal {
			return fmt.Errorf("image.source '%s' is invalid (must be 'registry' or 'local')", i.Source)
		}
	}

	if strings.ContainsAny(i.Tag, " \t\n\r") {
		return fmt.Errorf("image.tag '%s' contains whitespace", i.Tag)
	}

	if i.History != nil {
		if err := i.History.Validate(); err != nil {
			return err
		}

		if i.History.Strategy == HistoryStrategyRegistry {
			// Prevent mutable tags with registry strategy
			tag := strings.TrimSpace(i.Tag)
			if tag == "" || tag == "latest" {
				return fmt.Errorf("image.tag cannot be 'latest' or empty with registry strategy - use immutable tags like 'v1.2.3'")
			}

			mutableTags := []string{"main", "master", "develop", "staging", "production"}
			for _, mutable := range mutableTags {
				if tag == mutable {
					return fmt.Errorf("image.tag '%s' is mutable and not recommended with registry strategy - use immutable tags like 'v1.2.3'", tag)
				}
			}
		}
	}

	if i.RegistryAuth != nil {
		reg := i.RegistryAuth
		// Server is optional; if empty, it will be parsed from Repository at runtime.
		if strings.TrimSpace(reg.Server) != "" && strings.ContainsAny(reg.Server, " \t\n\r") {
			return fmt.Errorf("image.registry.server '%s' contains whitespace", reg.Server)
		}
		if err := reg.Username.Validate(); err != nil {
			return err
		}
		if err := reg.Password.Validate(); err != nil {
			return err
		}
	}
	return nil
}

type HistoryStrategy string

const (
	HistoryStrategyLocal    HistoryStrategy = "local"    // Keep images locally (default)
	HistoryStrategyRegistry HistoryStrategy = "registry" // Rely on registry tags
	HistoryStrategyNone     HistoryStrategy = "none"     // No rollback support
)

type ImageHistory struct {
	Strategy HistoryStrategy `json:"strategy" yaml:"strategy" toml:"strategy"`
	Count    *int            `json:"count,omitempty" yaml:"count,omitempty" toml:"count,omitempty"`
	Pattern  string          `json:"pattern,omitempty" yaml:"pattern,omitempty" toml:"pattern,omitempty"`
}

func (h *ImageHistory) Validate() error {
	if h.Strategy != "" {
		validStrategies := []HistoryStrategy{HistoryStrategyLocal, HistoryStrategyRegistry, HistoryStrategyNone}
		if !slices.Contains(validStrategies, h.Strategy) {
			return fmt.Errorf("image.history.strategy '%s' is invalid (must be 'local', 'registry', or 'none')", h.Strategy)
		}
	}

	// Count is required for both local and registry strategies
	if h.Strategy == HistoryStrategyLocal || h.Strategy == HistoryStrategyRegistry {
		if h.Count == nil {
			return fmt.Errorf("image.history.count is required for %s strategy", h.Strategy)
		}
		if *h.Count < 1 {
			return fmt.Errorf("image.history.count must be at least 1 for %s strategy", h.Strategy)
		}
	}

	// Pattern validation for registry strategy
	if h.Strategy == HistoryStrategyRegistry && strings.TrimSpace(h.Pattern) == "" {
		return fmt.Errorf("image.history.pattern is required for registry strategy")
	}

	return nil
}

func (b *Builder) Validate(format string) error {
	if b == nil {
		return nil
	}

	// TODO: add more validation, Dockerfile, Context, Platform

	for i, arg := range b.Args {
		if err := arg.Validate(format); err != nil {
			return fmt.Errorf("args[%d]: %w", i, err)
		}
	}

	return nil
}

type Builder struct {
	Context    string     `json:"context,omitempty" yaml:"context,omitempty" toml:"context,omitempty"`
	Dockerfile string     `json:"dockerfile,omitempty" yaml:"dockerfile,omitempty" toml:"dockerfile,omitempty"`
	Platform   string     `json:"platform,omitempty" yaml:"platform,omitempty" toml:"platform,omitempty"`
	Args       []BuildArg `json:"args,omitempty" yaml:"args,omitempty" toml:"args,omitempty"`
}

type BuildArg struct {
	Name        string `json:"name" yaml:"name" toml:"name"`
	ValueSource `mapstructure:",squash" json:",inline" yaml:",inline" toml:",inline"`
}

func (ba *BuildArg) Validate(format string) error {
	if ba.Name == "" {
		return errors.New("build argument 'name' cannot be empty")
	}

	if err := ba.ValueSource.Validate(); err != nil {
		return fmt.Errorf("build argument '%s': %w", ba.Name, err)
	}

	return nil
}
