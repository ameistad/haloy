package appconfigloader

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"

	"github.com/ameistad/haloy/internal/config"
	"github.com/go-viper/mapstructure/v2"
	"github.com/jinzhu/copier"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
)

func LoadImproved(
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

type AppConfigTarget struct {
	ResolvedAppConfig config.AppConfig // with secrets resolved
	RawAppConfig      config.AppConfig
}

func CreateTargets(rawAppConfig, resolvedAppConfig config.AppConfig) ([]AppConfigTarget, error) {
	var appConfigTargets []AppConfigTarget

	if len(resolvedAppConfig.Targets) > 0 {
		// Multi-target deployment
		for targetName, resolvedTarget := range resolvedAppConfig.Targets {
			rawTarget, exists := rawAppConfig.Targets[targetName]
			if !exists {
				return nil, fmt.Errorf("target '%s' exists in resolved config but not in raw config", targetName)
			}

			mergedRawAppConfig, err := rawAppConfig.MergeWithTarget(targetName, rawTarget)
			if err != nil {
				return nil, fmt.Errorf("failed to merge raw config for target '%s': %w", targetName, err)
			}

			mergedResolvedAppConfig, err := resolvedAppConfig.MergeWithTarget(targetName, resolvedTarget)
			if err != nil {
				return nil, fmt.Errorf("failed to merge resolved config for target '%s': %w", targetName, err)
			}

			appConfigTargets = append(appConfigTargets, AppConfigTarget{
				RawAppConfig:      *mergedRawAppConfig,
				ResolvedAppConfig: *mergedResolvedAppConfig,
			})
		}
	} else {
		// Single-target deployment
		appConfigTargets = append(appConfigTargets, AppConfigTarget{
			RawAppConfig:      rawAppConfig,
			ResolvedAppConfig: resolvedAppConfig,
		})
	}

	return appConfigTargets, nil
}

func Load(ctx context.Context, configPath string, targets []string, allTargets bool) (
	appConfigTargets []AppConfigTarget,
	baseAppConfig config.AppConfig,
	err error,
) {
	rawAppConfig, format, err := LoadRawAppConfig(configPath)
	if err != nil {
		return nil, config.AppConfig{}, err
	}

	// Set default values
	rawAppConfig.Normalize()

	if err := rawAppConfig.Validate(format); err != nil {
		return nil, config.AppConfig{}, err
	}

	// Set the format the app config was loaded in
	rawAppConfig.Format = format

	isMultiTarget := len(rawAppConfig.Targets) > 0

	var targetsToProcess []string

	if isMultiTarget {
		if len(targets) == 0 && !allTargets {
			var availableTargets []string
			for name := range rawAppConfig.Targets {
				availableTargets = append(availableTargets, name)
			}
			return nil, config.AppConfig{}, fmt.Errorf("multiple targets available (%s); please specify targets with --targets or use --all", strings.Join(availableTargets, ", "))
		}
		if allTargets {
			// User wants all available targets.
			for name := range rawAppConfig.Targets {
				targetsToProcess = append(targetsToProcess, name)
			}
		} else {
			// User specified a subset of targets.
			for _, name := range targets {
				if _, exists := rawAppConfig.Targets[name]; !exists {
					return nil, config.AppConfig{}, fmt.Errorf("target '%s' not found in configuration", name)
				}
			}
			targetsToProcess = targets
		}
	} else {

		if len(targets) > 0 || allTargets {
			return nil, config.AppConfig{}, fmt.Errorf("the --targets and --all flags are not applicable for a single-target configuration file")
		}
		targetsToProcess = []string{""} // Use an empty string to signify processing the base config itself.
	}

	// Deep copy to preserve original so we can return hooks
	if err := copier.Copy(&baseAppConfig, &rawAppConfig); err != nil {
		return nil, config.AppConfig{}, fmt.Errorf("failed to copy base app config: %w", err)
	}

	// Resolve secrets in registry auth, environment variables and build args
	resolvedAppConfig, err := ResolveSecrets(ctx, rawAppConfig)
	if err != nil {
		return nil, config.AppConfig{}, err
	}

	rawAppConfig.GlobalPreDeploy = nil
	rawAppConfig.GlobalPostDeploy = nil
	resolvedAppConfig.GlobalPreDeploy = nil
	resolvedAppConfig.GlobalPostDeploy = nil

	var finalAppConfigTargets []AppConfigTarget
	for _, targetName := range targetsToProcess {
		var rawTargetAppConfig *config.AppConfig
		var resolvedTargetAppConfig *config.AppConfig

		if isMultiTarget {
			rawOverrides := rawAppConfig.Targets[targetName]
			resolvedOverrides := resolvedAppConfig.Targets[targetName]

			rawTargetAppConfig, err = rawAppConfig.MergeWithTarget(targetName, rawOverrides)
			if err != nil {
				return nil, config.AppConfig{}, fmt.Errorf("unable to resolve raw config for '%s': %w", targetName, err)
			}
			resolvedTargetAppConfig, err = resolvedAppConfig.MergeWithTarget(targetName, resolvedOverrides)
			if err != nil {
				return nil, config.AppConfig{}, fmt.Errorf("unable to resolve resolved config for '%s': %w", targetName, err)
			}
		} else {
			rawTargetAppConfig = &rawAppConfig
			resolvedTargetAppConfig = &resolvedAppConfig
		}

		finalAppConfigTargets = append(finalAppConfigTargets, AppConfigTarget{
			RawAppConfig:      *rawTargetAppConfig,
			ResolvedAppConfig: *resolvedTargetAppConfig,
		})
	}

	return finalAppConfigTargets, baseAppConfig, nil
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
