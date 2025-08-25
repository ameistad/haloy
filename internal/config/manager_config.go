package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ameistad/haloy/internal/constants"
	"github.com/ameistad/haloy/internal/helpers"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
	"gopkg.in/yaml.v3"
)

type ManagerConfig struct {
	API struct {
		Domain string `json:"domain" yaml:"domain" toml:"domain"`
	} `json:"api" yaml:"api" toml:"api"`
	Certificates struct {
		AcmeEmail string `json:"acmeEmail" yaml:"acme_email" toml:"acme_email"`
	} `json:"certificates" yaml:"certificates" toml:"certificates"`
}

// Normalize sets default values for ManagerConfig
func (mc *ManagerConfig) Normalize() *ManagerConfig {
	// Add any defaults if needed in the future
	return mc
}

func (mc *ManagerConfig) Validate() error {
	if mc.API.Domain != "" {
		if err := helpers.IsValidDomain(mc.API.Domain); err != nil {
			return fmt.Errorf("invalid domain format: %w", err)
		}
	}

	if mc.Certificates.AcmeEmail != "" && !helpers.IsValidEmail(mc.Certificates.AcmeEmail) {
		return fmt.Errorf("invalid acme-email format: %s", mc.Certificates.AcmeEmail)
	}

	if mc.API.Domain != "" && mc.Certificates.AcmeEmail == "" {
		return fmt.Errorf("acmeEmail is required when domain is specified")
	}

	return nil
}

func LoadManagerConfig(path string) (*ManagerConfig, error) {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil, nil
	}

	format, err := getConfigFormat(path)
	if err != nil {
		return nil, err
	}

	parser, err := getConfigParser(format)
	if err != nil {
		return nil, err
	}

	k := koanf.New(".")
	if err := k.Load(file.Provider(path), parser); err != nil {
		return nil, fmt.Errorf("failed to load manager config file: %w", err)
	}

	var managerConfig ManagerConfig
	if err := k.UnmarshalWithConf("", &managerConfig, koanf.UnmarshalConf{Tag: format}); err != nil {
		return nil, fmt.Errorf("failed to unmarshal manager config: %w", err)
	}
	return &managerConfig, nil
}

func SaveManagerConfig(config *ManagerConfig, path string) error {
	ext := filepath.Ext(path)
	var data []byte
	var err error

	switch ext {
	case ".json":
		data, err = json.MarshalIndent(config, "", "  ")
	default: // yaml
		data, err = yaml.Marshal(config)
	}

	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	return os.WriteFile(path, data, constants.ModeFileDefault)
}
