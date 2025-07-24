package config

import (
	"fmt"

	"github.com/ameistad/haloy/internal/helpers"
	"github.com/spf13/viper"
)

type ManagerConfig struct {
	API struct {
		Domain string `yaml:"domain" json:"domain"`
	} `yaml:"api" json:"api"`
	Certificates struct {
		AcmeEmail string `yaml:"acmeEmail" json:"acmeEmail"`
	} `yaml:"certificates" json:"certificates"`
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
	v := viper.New()
	v.SetConfigFile(configPath)

	if err := v.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("failed to read manager config file: %w", err)
	}

	var managerConfig ManagerConfig
	if err := v.Unmarshal(&managerConfig); err != nil {
		return nil, fmt.Errorf("failed to unmarshal manager config: %w", err)
	}

	return &managerConfig, nil
}

func SaveManagerConfig(config *ManagerConfig, configPath string) error {
	v := viper.New()
	v.SetConfigFile(configPath)
	v.SetConfigType("yaml")

	v.Set("api.domain", config.API.Domain)
	v.Set("certificates.acmeEmail", config.Certificates.AcmeEmail)

	if err := v.WriteConfig(); err != nil {
		return fmt.Errorf("failed to write manager config: %w", err)
	}

	return nil
}
