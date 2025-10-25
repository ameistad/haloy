package appconfigloader

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strings"

	"github.com/ameistad/haloy/internal/config"
	"github.com/go-viper/mapstructure/v2"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
)

func Load(
	ctx context.Context,
	configPath string,
	targets []string,
	allTargets bool,
) (config.AppConfig, error) {
	rawAppConfig, format, err := LoadRawAppConfig(configPath)
	if err != nil {
		return config.AppConfig{}, err
	}

	rawAppConfig.Format = format
	rawAppConfig.Normalize()

	if len(rawAppConfig.Targets) > 0 { // is multi target

		if len(targets) == 0 && !allTargets {
			return config.AppConfig{}, errors.New("multiple targets available, please specify targets with --targets or use --all")
		}

		if len(targets) > 0 {

			filteredTargets := make(map[string]*config.TargetConfig)
			for _, targetName := range targets {
				if _, exists := rawAppConfig.Targets[targetName]; exists {
					filteredTargets[targetName] = rawAppConfig.Targets[targetName]
				} else {
					return config.AppConfig{}, fmt.Errorf("target '%s' not found in configuration", targetName)
				}
			}
			rawAppConfig.Targets = filteredTargets
		}

	} else {
		if len(targets) > 0 || allTargets {
			return config.AppConfig{}, errors.New("the --targets and --all flags are not applicable for a single-target configuration file")
		}
	}

	if err := rawAppConfig.Validate(format); err != nil {
		return config.AppConfig{}, err
	}

	return rawAppConfig, nil
}

func ResolveTargets(appConfig config.AppConfig) ([]config.AppConfig, error) {
	var resolvedConfigs []config.AppConfig

	if len(appConfig.Targets) > 0 {
		// Multi-target deployment - sort target names for deterministic order
		targetNames := make([]string, 0, len(appConfig.Targets))
		for targetName := range appConfig.Targets {
			targetNames = append(targetNames, targetName)
		}
		sort.Strings(targetNames) // Alphabetical order for consistency

		for _, targetName := range targetNames {
			target := appConfig.Targets[targetName]
			mergedConfig, err := appConfig.ResolveTarget(targetName, target)
			if err != nil {
				return nil, fmt.Errorf("failed to resolve target '%s': %w", targetName, err)
			}
			resolvedConfigs = append(resolvedConfigs, *mergedConfig)
		}
	} else {
		// Single-target deployment
		resolvedConfigs = append(resolvedConfigs, appConfig)
	}

	return resolvedConfigs, nil
}

func LoadRawAppConfig(configPath string) (config.AppConfig, string, error) {
	configFile, err := FindConfigFile(configPath)
	if err != nil {
		return config.AppConfig{}, "", err
	}

	format, err := config.GetConfigFormat(configFile)
	if err != nil {
		return config.AppConfig{}, "", err
	}

	parser, err := config.GetConfigParser(format)
	if err != nil {
		return config.AppConfig{}, "", err
	}

	k := koanf.New(".")
	if err := k.Load(file.Provider(configFile), parser); err != nil {
		return config.AppConfig{}, "", fmt.Errorf("failed to load config file: %w", err)
	}

	configKeys := k.Keys()
	appConfigType := reflect.TypeOf(config.AppConfig{})

	if err := config.CheckUnknownFields(appConfigType, configKeys, format); err != nil {
		return config.AppConfig{}, "", err
	}

	var appConfig config.AppConfig
	decoderConfig := &mapstructure.DecoderConfig{
		TagName: format,
		Result:  &appConfig,
		// This ensures that embedded structs with inline tags work properly
		Squash:     true,
		DecodeHook: config.PortDecodeHook(),
	}

	unmarshalConf := koanf.UnmarshalConf{
		Tag:           format,
		DecoderConfig: decoderConfig,
	}

	if err := k.UnmarshalWithConf("", &appConfig, unmarshalConf); err != nil {
		return config.AppConfig{}, "", fmt.Errorf("failed to unmarshal config: %w", err)
	}

	appConfig.Normalize()

	if err := appConfig.Validate(format); err != nil {
		return config.AppConfig{}, format, err
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
