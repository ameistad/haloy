package haloy

import (
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ameistad/haloy/internal/config"
	"github.com/ameistad/haloy/internal/constants"
	"github.com/ameistad/haloy/internal/helpers"
	"github.com/oklog/ulid"
)

func createDeploymentID() string {
	entropy := ulid.Monotonic(rand.New(rand.NewSource(time.Now().UnixNano())), 0)
	return ulid.MustNew(ulid.Timestamp(time.Now()), entropy).String()
}

func getToken(appConfig *config.AppConfig, url string) (string, error) {
	// Check for app-specific token env var
	if appConfig != nil && appConfig.APITokenEnv != "" {
		if token := os.Getenv(appConfig.APITokenEnv); token != "" {
			return token, nil
		}
		return "", fmt.Errorf("api token defined in config not found: %s environment variable not set", appConfig.APITokenEnv)
	}

	configDir, err := config.ConfigDir()
	if err != nil {
		return "", err
	}
	clientConfigPath := filepath.Join(configDir, constants.ClientConfigFileName)
	clientConfig, err := config.LoadClientConfig(clientConfigPath)
	if err != nil {
		return "", err
	}

	if clientConfig == nil {
		return "", fmt.Errorf("no client configuration found. Run: haloy server add <url> <token>")
	}

	normalizedURL, err := helpers.NormalizeServerURL(url)
	if err != nil {
		return "", err
	}

	serverConfig, exists := clientConfig.Servers[normalizedURL]
	if !exists {
		return "", fmt.Errorf("server %s not configured. Run: haloy server add %s <token>", normalizedURL, normalizedURL)
	}

	token := os.Getenv(serverConfig.TokenEnv)
	if token == "" {
		return "", fmt.Errorf("token not found for server %s. Please set environment variable: %s", normalizedURL, serverConfig.TokenEnv)
	}

	return token, nil
}

type ExpandedTarget struct {
	TargetName string
	Config     config.AppConfig
}

func expandTargets(appConfig config.AppConfig, targets []string, allTargets bool) ([]ExpandedTarget, error) {
	if len(appConfig.Targets) > 0 {
		if len(targets) == 0 && !allTargets {
			var availableTargets []string
			for name := range appConfig.Targets {
				availableTargets = append(availableTargets, name)
			}
			return nil, fmt.Errorf("multiple targets available (%s). Please specify targets with --targets or use --all to deploy to all targets",
				strings.Join(availableTargets, ", "))
		}
		if len(targets) > 0 && allTargets {
			return nil, fmt.Errorf("cannot specify both --target and --all flags")
		}

		var targetsToDeploy []string
		if len(targets) > 0 {
			// User specified one or more targets.
			for _, targetName := range targets {
				if _, exists := appConfig.Targets[targetName]; !exists {
					return nil, fmt.Errorf("target '%s' not found in configuration", targetName)
				}
				targetsToDeploy = append(targetsToDeploy, targetName)
			}
		} else {
			// allTargets is true
			for name := range appConfig.Targets {
				targetsToDeploy = append(targetsToDeploy, name)
			}
		}

		var targets []ExpandedTarget
		for _, name := range targetsToDeploy {
			overrides := appConfig.Targets[name]
			config := appConfig.MergeWithTarget(overrides)
			targets = append(targets, ExpandedTarget{
				TargetName: name,
				Config:     *config,
			})
		}

		return targets, nil
	}

	// Single target deployment (no targets section, just base config)
	if appConfig.Server != "" {
		if len(targets) > 0 || allTargets {
			return nil, fmt.Errorf("the --targets and --all flags are not applicable because the configuration file does not define a 'targets' section")
		}

		finalConfig := appConfig
		finalConfig.Targets = nil
		return []ExpandedTarget{{
			TargetName: "",
			Config:     finalConfig,
		}}, nil
	}

	return nil, fmt.Errorf("no server or targets are defined in the configuration")
}
