package haloy

import (
	"fmt"
	"path/filepath"

	"github.com/ameistad/haloy/internal/config"
	"github.com/ameistad/haloy/internal/constants"
)

func getServer(appConfig *config.AppConfig, serverURL string) (string, error) {
	if serverURL != "" {
		return serverURL, nil
	}
	if appConfig != nil && appConfig.Server != "" {
		return appConfig.Server, nil
	}

	configDir, err := config.ConfigDir()
	if err != nil {
		return "", err
	}
	clientConfigPath := filepath.Join(configDir, constants.ClientConfigFileName)
	clientConfig, _ := config.LoadClientConfig(clientConfigPath)
	if clientConfig != nil && clientConfig.DefaultServer != "" {
		return clientConfig.DefaultServer, nil
	}

	return "", fmt.Errorf("no server URL found")
}
