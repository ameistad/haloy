package config

import (
	"fmt"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/knadh/koanf/parsers/json"
	"github.com/knadh/koanf/parsers/toml"
	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/v2"
)

func getConfigFormat(configFile string) (string, error) {
	ext := filepath.Ext(configFile)
	switch ext {
	case ".json":
		return "json", nil
	case ".yaml", ".yml":
		return "yaml", nil
	case ".toml":
		return "toml", nil
	default:
		return "", fmt.Errorf("unsupported config file type: %s", ext)
	}
}

func getConfigParser(format string) (koanf.Parser, error) {
	var parser koanf.Parser
	switch format {
	case "json":
		parser = json.Parser()
	case "yaml":
		parser = yaml.Parser()
	case "toml":
		parser = toml.Parser()
	default:
		return nil, fmt.Errorf("unsupported config file type: %s", format)
	}
	return parser, nil
}

// getFieldNameForFormat returns the field name as it appears in the specified format
func getFieldNameForFormat(v any, fieldName, format string) string {
	t := reflect.TypeOf(v)
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}

	field, found := t.FieldByName(fieldName)
	if !found {
		return fieldName // fallback to Go field name
	}

	var tag string
	switch strings.ToLower(format) {
	case "json":
		tag = field.Tag.Get("json")
	case "yaml", "yml":
		tag = field.Tag.Get("yaml")
	case "toml":
		tag = field.Tag.Get("toml")
	default:
		return fieldName // fallback to Go field name
	}

	if tag == "" || tag == "-" {
		return fieldName // fallback if no tag
	}

	// Split on comma to handle tags like `json:"name,omitempty"`
	parts := strings.Split(tag, ",")
	return parts[0]
}
