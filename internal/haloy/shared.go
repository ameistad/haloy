package haloy

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ameistad/haloy/internal/config"
	"github.com/ameistad/haloy/internal/constants"
	"github.com/ameistad/haloy/internal/helpers"
)

func getToken(url string) (string, error) {
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
		for url, _ := range clientConfig.Servers {
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
