package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/ameistad/haloy/internal/constants"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
	"gopkg.in/yaml.v3"
)

type ClientConfig struct {
	Servers map[string]string `json:"servers" yaml:"servers" toml:"servers"`
}

func (cc *ClientConfig) AddServer(url, token string) {
	if cc.Servers == nil {
		cc.Servers = make(map[string]string)
	}
	cc.Servers[url] = token
}

func (cc *ClientConfig) RemoveServer(url string) error {
	if _, exists := cc.Servers[url]; !exists {
		return fmt.Errorf("server %s not found", url)
	}
	delete(cc.Servers, url)
	return nil
}

func (cc *ClientConfig) ListServers() []string {
	var urls []string
	for url := range cc.Servers {
		urls = append(urls, url)
	}
	sort.Strings(urls)
	return urls
}

func LoadClientConfig(path string) (*ClientConfig, error) {
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
		return nil, fmt.Errorf("failed to load client config file: %w", err)
	}

	var clientConfig ClientConfig
	if err := k.UnmarshalWithConf("", &clientConfig, koanf.UnmarshalConf{Tag: format}); err != nil {
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
