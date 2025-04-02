package config

import (
	"fmt"
	"os"
	"reflect"
	"strings"

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

// TODO: implement this
type ImageSource struct {
	Repository string `yaml:"repository"`
	Tag        string `yaml:"tag, omitempty"`
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
		if imgSource.Repository == "" {
			return fmt.Errorf("source.image.repository is required")
		}
		// Optional: Add regex validation for imgSource.Repository and imgSource.Tag if needed.
		// Example simple check: prevent whitespace
		if strings.ContainsAny(imgSource.Repository, " \t\n\r") {
			return fmt.Errorf("source.image.repository '%s' contains whitespace", imgSource.Repository)
		}
		if strings.ContainsAny(imgSource.Tag, " \t\n\r") {
			return fmt.Errorf("source.image.tag '%s' contains whitespace", imgSource.Tag)
		}
	}

	// Check if *at least one* source type was defined
	if !sourceIsDefined {
		return fmt.Errorf("source must contain either 'dockerfile' or 'image'")
	}
	return nil
}
