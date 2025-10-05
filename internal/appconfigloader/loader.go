package appconfigloader

import (
	"context"
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

type AppConfigTarget struct {
	ResolvedAppConfig config.AppConfig // with secrets resolved
	RawAppConfig      config.AppConfig
}

func Load(ctx context.Context, configPath string, targets []string, allTargets bool) (
	appConfigTargets []AppConfigTarget,
	baseAppConfig config.AppConfig,
	err error,
) {
	rawAppConfig, format, err := loadRawAppConfig(configPath)
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

	// Deep copy to preserve original so we can return hooks
	if err := copier.Copy(&baseAppConfig, &rawAppConfig); err != nil {
		return nil, config.AppConfig{}, fmt.Errorf("failed to copy base app config: %w", err)
	}

	// Resolve secrets and environment variables.
	resolvedAppConfig, err := ResolveSecrets(ctx, rawAppConfig)
	if err != nil {
		return nil, config.AppConfig{}, err
	}

	rawAppConfig.GlobalPreDeploy = nil
	rawAppConfig.GlobalPostDeploy = nil
	resolvedAppConfig.GlobalPreDeploy = nil
	resolvedAppConfig.GlobalPostDeploy = nil

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
		// This is a single-target configuration file.
		if len(targets) > 0 || allTargets {
			return nil, config.AppConfig{}, fmt.Errorf("the --targets and --all flags are not applicable for a single-target configuration file")
		}
		targetsToProcess = []string{""} // Use an empty string to signify processing the base config itself.
	}

	var finalAppConfigTargets []AppConfigTarget
	for _, target := range targetsToProcess {
		var rawTargetAppConfig *config.AppConfig
		var resolvedTargetAppConfig *config.AppConfig

		if isMultiTarget {
			rawOverrides := rawAppConfig.Targets[target]
			resolvedOverrides := resolvedAppConfig.Targets[target]

			rawTargetAppConfig = rawAppConfig.MergeWithTarget(rawOverrides)
			resolvedTargetAppConfig = resolvedAppConfig.MergeWithTarget(resolvedOverrides)
		} else {
			rawTargetAppConfig = &rawAppConfig
			resolvedTargetAppConfig = &resolvedAppConfig
		}

		rawTargetAppConfig.TargetName = target
		resolvedTargetAppConfig.TargetName = target

		finalAppConfigTargets = append(finalAppConfigTargets, AppConfigTarget{
			RawAppConfig:      *rawTargetAppConfig,
			ResolvedAppConfig: *resolvedTargetAppConfig,
		})
	}

	return finalAppConfigTargets, baseAppConfig, nil
}

func loadRawAppConfig(configPath string) (config.AppConfig, string, error) {
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
