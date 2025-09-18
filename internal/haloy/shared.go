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
	"github.com/ameistad/haloy/internal/ui"
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

func getServer(appConfig *config.AppConfig, url string) (string, error) {
	// Explicit server URL parameter takes highest priority
	if url != "" {
		return url, nil
	}

	// Server specified in app config
	if appConfig != nil && appConfig.Server != "" {
		return appConfig.Server, nil
	}

	// Fall back to client config for default server discovery
	configDir, err := config.ConfigDir()
	if err != nil {
		return "", err
	}
	clientConfigPath := filepath.Join(configDir, constants.ClientConfigFileName)
	clientConfig, _ := config.LoadClientConfig(clientConfigPath)

	if clientConfig == nil || len(clientConfig.Servers) == 0 {
		return "", fmt.Errorf("no servers configured. Run: haloy server add <url> <token>")
	}

	// If only one server configured, use it as default
	if len(clientConfig.Servers) == 1 {
		for url := range clientConfig.Servers {
			return url, nil
		}
	}

	// Multiple servers but no default specified
	var urls []string
	for url := range clientConfig.Servers {
		urls = append(urls, url)
	}

	return "", fmt.Errorf("multiple servers configured but no server specified in config.\nAvailable servers: %s\nAdd 'server: <url>' to your app config", strings.Join(urls, ", "))
}

type ExpandedTarget struct {
	TargetName string
	Config     config.AppConfig
}

func expandTargets(appConfig config.AppConfig, target string, allTargets bool) ([]ExpandedTarget, error) {
	if len(appConfig.Targets) > 0 {
		// Multiple targets available - user must specify intent
		if target == "" && !allTargets {
			var availableTargets []string
			for name := range appConfig.Targets {
				availableTargets = append(availableTargets, name)
			}
			return nil, fmt.Errorf("multiple targets available (%s). Please specify a target with --target or use --all to deploy to all targets",
				strings.Join(availableTargets, ", "))
		}

		if target != "" && allTargets {
			return nil, fmt.Errorf("cannot specify both --target and --all flags")
		}

		var targetsToDeploy []string
		if target != "" {
			if _, exists := appConfig.Targets[target]; !exists {
				return nil, fmt.Errorf("target '%s' not found in configuration", target)
			}
			targetsToDeploy = []string{target}
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
		// For single target, warn if unnecessary flags are used
		if target != "" || allTargets {
			// You might want to return an error or just warn and continue
			// I'll show a warning approach, but you could make this an error
			ui.Warn("--target and --all flags are ignored when no targets are defined")
		}

		finalConfig := appConfig
		finalConfig.Targets = nil
		return []ExpandedTarget{{
			TargetName: "default",
			Config:     finalConfig,
		}}, nil
	}

	return nil, fmt.Errorf("no server or targets are defined in the configuration")
}
