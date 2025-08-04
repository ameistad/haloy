package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ameistad/haloy/internal/helpers"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
	"gopkg.in/yaml.v3"
)

type ManagerConfig struct {
	API struct {
		Domain string `yaml:"domain" json:"domain" koanf:"domain"`
	} `yaml:"api" json:"api" koanf:"api"`
	Certificates struct {
		AcmeEmail string `yaml:"acme_email" json:"acmeEmail" koanf:"acmeEmail"`
	} `yaml:"certificates" json:"certificates" koanf:"certificates"`
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

func LoadManagerConfig(configPath string) (*ManagerConfig, error) {
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		return nil, nil
	}
	k := koanf.New(".")
	parser, err := getConfigParser(configPath)
	if err != nil {
		return nil, err
	}
	if err := k.Load(file.Provider(configPath), parser); err != nil {
		return nil, fmt.Errorf("failed to load manager config file: %w", err)
	}

	var managerConfig ManagerConfig

	if err := k.Unmarshal("", &managerConfig); err != nil {
		return nil, fmt.Errorf("failed to unmarshal manager config: %w", err)
	}
	return &managerConfig, nil
}

func Save(config *ManagerConfig, configPath string) error {
	ext := filepath.Ext(configPath)
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

	return os.WriteFile(configPath, data, 0644)
}
