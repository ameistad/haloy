package config

import (
	"fmt"
	"path/filepath"

	"github.com/knadh/koanf/parsers/json"
	"github.com/knadh/koanf/parsers/toml"
	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/v2"
)

func getConfigParser(configFile string) (koanf.Parser, error) {
	var parser koanf.Parser
	ext := filepath.Ext(configFile)
	switch ext {
	case ".json":
		parser = json.Parser()
	case ".yaml", ".yml":
		parser = yaml.Parser()
	case ".toml":
		parser = toml.Parser()
	default:
		return nil, fmt.Errorf("unsupported config file type: %s", ext)
	}
	return parser, nil
}
