package configresolver

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/ameistad/haloy/internal/config"
	"github.com/jinzhu/copier"
)

func Resolve(ctx context.Context, unresolvedConfig *config.AppConfig, configFormat string) (*config.AppConfig, error) {
	if unresolvedConfig == nil {
		return nil, nil
	}

	// Copy to avoid mutating the original
	resolvedConfig := &config.AppConfig{}
	err := copier.Copy(resolvedConfig, unresolvedConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to copy config for resolution: %w", err)
	}

	sourcesToResolve := gatherValueSources(resolvedConfig)
	if len(sourcesToResolve) == 0 {
		return resolvedConfig, nil
	}

	// Group sources by the provider and the specific source configuration.
	// This is the core of the "Fetch & Extract" model, ensuring we only call each provider once per unique source.
	groupedSources, err := groupSources(sourcesToResolve, resolvedConfig.SecretProviders, configFormat)
	if err != nil {
		return nil, err
	}

	// Fetch the data for each group in bulk.
	// The cache's key is a unique identifier for the source (e.g., "onepassword:prod_api_keys").
	// The value is a map of the fetched secrets (e.g., {"api-key": "sk_...", "password": "..."}).
	fetchedDataCache, err := fetchGroupedSources(ctx, groupedSources)
	if err != nil {
		return nil, err
	}

	// Extract the values from the fetched data and update the config.
	if err := extractValues(sourcesToResolve, fetchedDataCache); err != nil {
		return nil, err
	}

	return resolvedConfig, nil
}

// gatherValueSources scans the AppConfig and collects pointers to all ValueSource instances that need to be resolved.
func gatherValueSources(appConfig *config.AppConfig) []*config.ValueSource {
	var sources []*config.ValueSource

	for i := range appConfig.Env {
		sources = append(sources, &appConfig.Env[i].ValueSource)
	}

	if appConfig.Image.RegistryAuth != nil {
		sources = append(sources, &appConfig.Image.RegistryAuth.Username)
		sources = append(sources, &appConfig.Image.RegistryAuth.Password)
	}

	return sources
}

// A unique key to identify a fetch operation (e.g., "onepassword:api_keys")
type groupKey string

// fetchGroup represents a single, bulk fetch operation to a provider.
type fetchGroup struct {
	provider   string // e.g., "onepassword"
	sourceName string // e.g., "api_keys"
	// The provider-specific configuration object
	sourceConfig any
	// The list of specific keys to extract from the fetched data
	keysToExtract map[string]bool
}

// groupSources organizes the ValueSource instances into bulk fetch operations.
func groupSources(sources []*config.ValueSource, providers *config.SecretProviders, configFormat string) (map[groupKey]fetchGroup, error) {
	groups := make(map[groupKey]fetchGroup)

	// if there are no providers are defined we'll check if there are any from.secret in the config and return an error.
	if providers == nil {
		for _, vs := range sources {
			if vs.From != nil && vs.From.Secret != "" {
				return nil, fmt.Errorf("found 'from.secret' reference but no '%s' block is defined in the configuration", config.GetFieldNameForFormat(config.AppConfig{}, "SecretProviders", configFormat))
			}
		}
		return groups, nil // Only `env:` sources are possible, which don't need grouping.
	}

	for _, vs := range sources {
		if vs.From == nil || vs.From.Secret == "" {
			continue // Skip plaintext values and 'env:' sources
		}

		parts := strings.SplitN(vs.From.Secret, ":", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid secret reference format: '%s'. Expected 'provider:source_name.key'", vs.From.Secret)
		}
		provider, ref := parts[0], parts[1]

		refParts := strings.SplitN(ref, ".", 2)
		if len(refParts) != 2 {
			return nil, fmt.Errorf("invalid secret reference format: '%s'. Expected 'source_name.key'", ref)
		}
		sourceName, extractKey := refParts[0], refParts[1]

		key := groupKey(fmt.Sprintf("%s:%s", provider, sourceName))
		group, ok := groups[key]
		if !ok {
			var sourceConfig any
			var found bool
			switch provider {
			case "onepassword":
				sourceConfig, found = providers.OnePassword[sourceName]
				// case "doppler":
				// 	sourceConfig, found = providers.Doppler[sourceName]
				// 	// Add cases for other providers here
			}

			if !found {
				return nil, fmt.Errorf("secret source '%s' for provider '%s' not defined in 'secretProviders' block", sourceName, provider)
			}

			group = fetchGroup{
				provider:      provider,
				sourceName:    sourceName,
				sourceConfig:  sourceConfig,
				keysToExtract: make(map[string]bool),
			}
		}

		group.keysToExtract[extractKey] = true
		groups[key] = group
	}

	return groups, nil
}

// fetchGroupedSources executes the bulk fetch for each group and returns a cache of the results.
func fetchGroupedSources(ctx context.Context, groups map[groupKey]fetchGroup) (map[groupKey]map[string]string, error) {
	cache := make(map[groupKey]map[string]string)

	for key, group := range groups {
		var fetchedSecrets map[string]string
		var err error

		switch group.provider {
		case "onepassword":
			cfg := group.sourceConfig.(config.OnePasswordSourceConfig)
			fetchedSecrets, err = fetchFrom1Password(ctx, cfg)
		// Add cases for other providers here
		default:
			err = fmt.Errorf("unsupported secret provider: %s", group.provider)
		}

		if err != nil {
			return nil, fmt.Errorf("failed to fetch secrets for source '%s': %w", group.sourceName, err)
		}
		cache[key] = fetchedSecrets
	}

	return cache, nil
}

// extractValues populates the final values into the config struct from the cache.
func extractValues(sources []*config.ValueSource, cache map[groupKey]map[string]string) error {
	for _, vs := range sources {
		if vs.From == nil {
			continue
		}

		if vs.From.Env != "" {
			vs.Value = os.Getenv(vs.From.Env)
		} else if vs.From.Secret != "" {
			parts := strings.SplitN(vs.From.Secret, ":", 2)
			provider, ref := parts[0], parts[1]
			refParts := strings.SplitN(ref, ".", 2)
			sourceName, extractKey := refParts[0], refParts[1]

			key := groupKey(fmt.Sprintf("%s:%s", provider, sourceName))

			fetchedGroup, ok := cache[key]
			if !ok {
				return fmt.Errorf("internal error: data for source '%s' not found in cache", sourceName)
			}

			value, ok := fetchedGroup[extractKey]
			if !ok {
				// To provide a better error message, list available keys
				availableKeys := make([]string, 0, len(fetchedGroup))
				for k := range fetchedGroup {
					availableKeys = append(availableKeys, k)
				}
				return fmt.Errorf("key '%s' not found in secret source '%s'. Available keys: %v", extractKey, sourceName, availableKeys)
			}
			vs.Value = value
		}

		// Clear the 'From' block now that it's resolved.
		vs.From = nil
	}
	return nil
}

func executeCommand(ctx context.Context, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	output, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.Error); ok && ee.Err == exec.ErrNotFound {
			return "", fmt.Errorf("command not found: '%s'. Is the required CLI tool installed and in your PATH?", name)
		}
		if exitErr, ok := err.(*exec.ExitError); ok {
			return "", fmt.Errorf("command '%s' failed: %s", name, string(exitErr.Stderr))
		}
		return "", fmt.Errorf("failed to execute command '%s': %w", name, err)
	}
	return strings.TrimSpace(string(output)), nil
}
