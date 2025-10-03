package configloader

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"github.com/ameistad/haloy/internal/config"
	appConfig "github.com/ameistad/haloy/internal/config/app"
	"github.com/go-viper/mapstructure/v2"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
)

// parseFile is the main entry point for this file's logic. It orchestrates finding,
// loading, parsing, and unmarshalling the config file into a raw AppConfig struct.
func parseFile(path string) (appConfig.AppConfig, string, error) {
	var appConfig appConfig.AppConfig

	configFile, err := FindConfigFile(path)
	if err != nil {
		return appConfig, "", err
	}

	format, err := config.GetConfigFormat(configFile)
	if err != nil {
		return appConfig, "", err
	}

	parser, err := config.GetConfigParser(format)
	if err != nil {
		return appConfig, "", err
	}

	k := koanf.New(".")
	if err := k.Load(file.Provider(configFile), parser); err != nil {
		return appConfig, "", fmt.Errorf("failed to load config file: %w", err)
	}

	if err := config.CheckUnknownFields(reflect.TypeOf(appConfig), k.Keys(), format); err != nil {
		return appConfig, "", err
	}

	decoderConfig := &mapstructure.DecoderConfig{
		TagName:    format,
		Result:     &appConfig,
		Squash:     true,
		DecodeHook: portDecodeHook(), // Assuming portDecodeHook is also moved here or made public
	}
	unmarshalConf := koanf.UnmarshalConf{
		Tag:           format,
		DecoderConfig: decoderConfig,
	}

	if err := k.UnmarshalWithConf("", &appConfig, unmarshalConf); err != nil {
		return appConfig, "", fmt.Errorf("failed to unmarshal config: %w", err)
	}

	return appConfig, format, nil
}

var (
	supportedExtensions  = []string{".json", ".yaml", ".yml", ".toml"}
	supportedConfigNames = []string{"haloy.json", "haloy.yaml", "haloy.yml", "haloy.toml"}
)

// FindConfigFile finds a haloy config file based on the given path
// It supports:
// - Full path to a config file
// - Directory containing a haloy config file
// - Relative paths
func FindConfigFile(path string) (string, error) {
	// If no path provided, use current directory
	if path == "" {
		path = "."
	}

	// Convert to absolute path
	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("failed to get absolute path: %w", err)
	}

	// Check if the path exists
	stat, err := os.Stat(absPath)
	if err != nil {
		return "", fmt.Errorf("path does not exist: %s", absPath)
	}

	// If it's a file, validate it's a supported extension
	if !stat.IsDir() {
		ext := filepath.Ext(absPath)
		if !slices.Contains(supportedExtensions, ext) {
			return "", fmt.Errorf("file %s is not a valid haloy config file (must be .json, .yaml, .yml, or .toml)", absPath)
		}
		return absPath, nil
	}

	// If it's a directory, look for haloy config files
	for _, configName := range supportedConfigNames {
		configPath := filepath.Join(absPath, configName)
		if _, err := os.Stat(configPath); err == nil {
			return configPath, nil
		}
	}

	// Get the directory name for the error (use base name if path is ".")
	dirName := path
	if path == "." {
		if cwd, err := os.Getwd(); err == nil {
			dirName = filepath.Base(cwd)
		}
	}

	return "", fmt.Errorf("no haloy config file found in directory %s (looking for: %s)",
		dirName, strings.Join(supportedConfigNames, ", "))
}

func portDecodeHook() mapstructure.DecodeHookFuncType {
	return func(
		f reflect.Type,
		t reflect.Type,
		data any,
	) (any, error) {
		// Only process if target type is Port
		if t != reflect.TypeOf(Port("")) {
			return data, nil
		}

		switch v := data.(type) {
		case string:
			return Port(v), nil
		case int:
			return Port(strconv.Itoa(v)), nil
		case int64:
			return Port(strconv.FormatInt(v, 10)), nil
		case float64:
			// Handle case where YAML/JSON might parse integers as floats
			if v == float64(int(v)) {
				return Port(strconv.Itoa(int(v))), nil
			}
			return nil, fmt.Errorf("port must be an integer, got float: %v", v)
		default:
			return nil, fmt.Errorf("port must be a string or integer, got %T: %v", data, data)
		}
	}
}
