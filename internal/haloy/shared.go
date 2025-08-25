package haloy

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/ameistad/haloy/internal/config"
	"github.com/ameistad/haloy/internal/constants"
)

func getServer(appConfig *config.AppConfig, serverURL string) (string, error) {
	// Explicit server URL parameter takes highest priority
	if serverURL != "" {
		return serverURL, nil
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
