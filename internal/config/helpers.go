package config

import "fmt"

func AppConfigByName(appName string) (*AppConfig, error) {
	configFilePath, err := ConfigFilePath()
	if err != nil {
		return nil, err
	}

	configFile, err := LoadAndValidateConfig(configFilePath)
	if err != nil {
		return nil, fmt.Errorf("configuration error: %w", err)
	}

	var appConfig *AppConfig
	for i := range configFile.Apps {
		if configFile.Apps[i].Name == appName {
			// Create a copy of the app config
			app := configFile.Apps[i]
			appConfig = &app
			break
		}
	}
	if appConfig == nil {
		return nil, fmt.Errorf("app '%s' not found in config", appName)
	}

	return appConfig, nil
}
