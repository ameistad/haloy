package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ameistad/haloy/internal/helpers"
	"github.com/spf13/viper"
	"gopkg.in/yaml.v3"
)

type ManagerConfig struct {
	API struct {
		Domain string `yaml:"domain" json:"domain" mapstructure:"domain"`
	} `yaml:"api" json:"api" mapstructure:"api"`
	Certificates struct {
		AcmeEmail string `yaml:"acme_email" json:"acmeEmail" mapstructure:"acme_email"` // Add mapstructure tag!
	} `yaml:"certificates" json:"certificates" mapstructure:"certificates"`
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
	v := viper.New()
	v.SetConfigFile(configPath)

	if err := v.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("failed to read manager config file: %w", err)
	}

	// Debug: Show ALL viper keys and values
	fmt.Printf("DEBUG: All viper keys: %v\n", v.AllKeys())
	fmt.Printf("DEBUG: Raw viper values:\n")
	fmt.Printf("  - api.domain: '%s'\n", v.GetString("api.domain"))
	fmt.Printf("  - certificates.acme_email: '%s'\n", v.GetString("certificates.acme_email"))
	fmt.Printf("  - certificates: %v\n", v.Get("certificates"))

	var managerConfig ManagerConfig
	if err := v.Unmarshal(&managerConfig); err != nil {
		return nil, fmt.Errorf("failed to unmarshal manager config: %w", err)
	}

	fmt.Printf("DEBUG: After unmarshal:\n")
	fmt.Printf("  - Domain: '%s'\n", managerConfig.API.Domain)
	fmt.Printf("  - Email: '%s'\n", managerConfig.Certificates.AcmeEmail)

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
