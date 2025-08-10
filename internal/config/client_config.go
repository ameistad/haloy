package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ameistad/haloy/internal/constants"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
	"gopkg.in/yaml.v3"
)

type ClientConfig struct {
	DefaultServer string `json:"defaultServer" yaml:"default_server" toml:"default_server"`
}

func LoadClientConfig(path string) (*ClientConfig, error) {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil, nil
	}
	k := koanf.New(".")
	parser, err := getConfigParser(path)
	if err != nil {
		return nil, err
	}
	if err := k.Load(file.Provider(path), parser); err != nil {
		return nil, fmt.Errorf("failed to load client config file: %w", err)
	}

	var clientConfig ClientConfig
	tag, err := getConfigTag(path)
	if err != nil {
		return nil, err
	}

	if err := k.UnmarshalWithConf("", &clientConfig, koanf.UnmarshalConf{Tag: tag}); err != nil {
		return nil, fmt.Errorf("failed to unmarshal client config: %w", err)
	}
	return &clientConfig, nil
}

func (cc *ClientConfig) Save(path string) error {
	ext := filepath.Ext(path)
	var data []byte
	var err error

	switch ext {
	case ".json":
		data, err = json.MarshalIndent(cc, "", "  ")
	default: // yaml
		data, err = yaml.Marshal(cc)
	}

	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	return os.WriteFile(path, data, constants.ModeFileDefault)
}
