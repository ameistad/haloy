package configloader

import (
	"context"
	"fmt"
	"strings"

	appConfig "github.com/ameistad/haloy/internal/config/app"
)

func Load(ctx context.Context, configPath string, targets []string, allTargets bool) (
	appConfigs []appConfig.AppConfig,
	globalPreDeploy []string,
	globalPostDeploy []string,
	format string,
	err error,
) {
	baseConfig, format, err := parseFile(configPath)
	if err != nil {
		return nil, nil, nil, "", err
	}
	globalPreDeploy = baseConfig.GlobalPreDeploy
	globalPostDeploy = baseConfig.GlobalPostDeploy
	baseConfig.GlobalPreDeploy = nil
	baseConfig.GlobalPostDeploy = nil

	isMultiTarget := len(baseConfig.Targets) > 0

	var finalConfigs []appConfig.AppConfig
	var targetsToProcess []string

	if isMultiTarget {
		if len(targets) == 0 && !allTargets {
			var availableTargets []string
			for name := range baseConfig.Targets {
				availableTargets = append(availableTargets, name)
			}
			return nil, nil, nil, format, fmt.Errorf("multiple targets available (%s); please specify targets with --targets or use --all", strings.Join(availableTargets, ", "))
		}
		if allTargets {
			// User wants all available targets.
			for name := range baseConfig.Targets {
				targetsToProcess = append(targetsToProcess, name)
			}
		} else {
			// User specified a subset of targets.
			for _, name := range targets {
				if _, exists := baseConfig.Targets[name]; !exists {
					return nil, nil, nil, format, fmt.Errorf("target '%s' not found in configuration", name)
				}
			}
			targetsToProcess = targets
		}
	} else {
		// This is a single-target configuration file.
		if len(targets) > 0 || allTargets {
			return nil, nil, nil, format, fmt.Errorf("the --targets and --all flags are not applicable for a single-target configuration file")
		}
		targetsToProcess = []string{""} // Use an empty string to signify processing the base config itself.
	}

	for _, target := range targetsToProcess {
		var mergedConfig *appConfig.AppConfig

		if isMultiTarget {
			overrides := baseConfig.Targets[target]
			mergedConfig = mergeWithTarget(&baseConfig, overrides)
		} else {
			mergedConfig = &baseConfig
		}

		mergedConfig.TargetName = target

		// Set default values
		mergedConfig.Normalize()

		//  Resolve secrets and environment variables.
		resolvedConfig, err := resolveSecrets(ctx, mergedConfig, format)
		if err != nil {
			errCtx := "failed to resolve secrets"
			if target != "" {
				errCtx = fmt.Sprintf("%s for target '%s'", errCtx, target)
			}
			return nil, nil, nil, format, fmt.Errorf("%s: %w", errCtx, err)
		}

		//  Validate the final, resolved configuration.
		if err := resolvedConfig.Validate(format); err != nil {
			errCtx := "validation failed"
			if target != "" {
				errCtx = fmt.Sprintf("%s for target '%s'", errCtx, target)
			}
			return nil, nil, nil, format, fmt.Errorf("%s: %w", errCtx, err)
		}

		finalConfigs = append(finalConfigs, *resolvedConfig)
	}

	return finalConfigs, globalPreDeploy, globalPostDeploy, format, nil
}

// mergeWithTarget creates a new AppConfig by applying a target's overrides to the base config.
func mergeWithTarget(appConfig *appConfig.AppConfig, targetConfig *appConfig.TargetConfig) *appConfig.AppConfig {
	mergedConfig := *appConfig

	if targetConfig == nil {
		mergedConfig.Targets = nil
		return &mergedConfig
	}

	// Apply overrides from the target. Target values take precedence.
	if targetConfig.Image.Repository != "" {
		mergedConfig.Image.Repository = targetConfig.Image.Repository
	}
	if targetConfig.Image.Tag != "" {
		mergedConfig.Image.Tag = targetConfig.Image.Tag
	}
	if targetConfig.Image.Source != "" {
		mergedConfig.Image.Source = targetConfig.Image.Source
	}
	if targetConfig.Image.History != nil {
		mergedConfig.Image.History = targetConfig.Image.History
	}
	if targetConfig.Image.RegistryAuth != nil {
		mergedConfig.Image.RegistryAuth = targetConfig.Image.RegistryAuth
	}
	if targetConfig.Server != "" {
		mergedConfig.Server = targetConfig.Server
	}
	if targetConfig.APITokenEnv != "" {
		mergedConfig.APITokenEnv = targetConfig.APITokenEnv
	}
	if targetConfig.Domains != nil {
		mergedConfig.Domains = targetConfig.Domains
	}
	if targetConfig.ACMEEmail != "" {
		mergedConfig.ACMEEmail = targetConfig.ACMEEmail
	}
	if targetConfig.Env != nil {
		mergedConfig.Env = targetConfig.Env
	}
	if targetConfig.HealthCheckPath != "" {
		mergedConfig.HealthCheckPath = targetConfig.HealthCheckPath
	}
	if targetConfig.Port != "" {
		mergedConfig.Port = targetConfig.Port
	}
	if targetConfig.Replicas != nil {
		mergedConfig.Replicas = targetConfig.Replicas
	}
	if targetConfig.Volumes != nil {
		mergedConfig.Volumes = targetConfig.Volumes
	}
	if targetConfig.NetworkMode != "" {
		mergedConfig.NetworkMode = targetConfig.NetworkMode
	}
	if targetConfig.PreDeploy != nil {
		mergedConfig.PreDeploy = targetConfig.PreDeploy
	}
	if targetConfig.PostDeploy != nil {
		mergedConfig.PostDeploy = targetConfig.PostDeploy
	}

	// The final, merged config has no concept of targets.
	mergedConfig.Targets = nil

	return &mergedConfig
}
