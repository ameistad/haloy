package config

import (
	"fmt"
	"path/filepath"
	"reflect"
	"slices"
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

func checkUnknownFields(structType reflect.Type, configKeys []string, format string) error {
	knownFields := getKnownFields(structType, format)

	unknownFields := make([]string, 0)
	for _, key := range configKeys {
		if !slices.Contains(knownFields, key) {
			unknownFields = append(unknownFields, key)
		}
	}

	if len(unknownFields) > 0 {
		return fmt.Errorf("unknown config fields found: %v", unknownFields)
	}

	return nil
}

// getFieldNameForFormat returns the field name as it appears in the specified format
func getKnownFields(structType reflect.Type, format string) []string {
	var fields []string
	collectFields(structType, format, "", &fields)
	return fields
}

// collectFields recursively collects field names from a struct type
func collectFields(structType reflect.Type, format string, prefix string, fields *[]string) {
	if structType.Kind() == reflect.Ptr {
		structType = structType.Elem()
	}
	if structType.Kind() != reflect.Struct {
		return
	}

	for i := 0; i < structType.NumField(); i++ {
		field := structType.Field(i)
		if !field.IsExported() {
			continue
		}

		fieldType := field.Type
		if fieldType.Kind() == reflect.Ptr {
			fieldType = fieldType.Elem()
		}

		//  Check for embedding.
		if field.Anonymous && fieldType.Kind() == reflect.Struct {
			// If it's an embedded struct, recurse into it with the SAME prefix
			// to "promote" its fields to the parent's level.
			collectFields(fieldType, format, prefix, fields)
			// Then, skip the rest of the loop for this field.
			continue
		}

		//  If it's NOT an embedded struct, proceed as normal.
		fieldName := getFieldTagName(field, format)
		if fieldName == "" || fieldName == "-" {
			continue
		}

		fullFieldName := fieldName
		if prefix != "" {
			fullFieldName = prefix + "." + fieldName
		}
		*fields = append(*fields, fullFieldName)

		//  Recurse into nested (but not embedded) structs/slices.
		if fieldType.Kind() == reflect.Struct {
			collectFields(fieldType, format, fullFieldName, fields)
		} else if fieldType.Kind() == reflect.Slice {
			elemType := fieldType.Elem()
			if elemType.Kind() == reflect.Ptr {
				elemType = elemType.Elem()
			}
			if elemType.Kind() == reflect.Struct {
				collectFields(elemType, format, fullFieldName, fields)
			}
		}
	}
}

// getFieldTagName returns the field name for the specified format from struct tags
func getFieldTagName(field reflect.StructField, format string) string {
	var tag string
	switch strings.ToLower(format) {
	case "json":
		tag = field.Tag.Get("json")
	case "yaml", "yml":
		tag = field.Tag.Get("yaml")
	case "toml":
		tag = field.Tag.Get("toml")
	default:
		// Fallback to the Go field name if no format specified
		return strings.ToLower(field.Name)
	}

	if tag == "" {
		// If no tag for this format, fallback to Go field name
		return strings.ToLower(field.Name)
	}

	// Handle tags like `json:"name,omitempty"`
	parts := strings.Split(tag, ",")
	tagName := parts[0]

	// Skip fields marked with "-"
	if tagName == "-" {
		return ""
	}

	return tagName
}
