package config

import (
	"fmt"
	"reflect"

	"github.com/go-viper/mapstructure/v2"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
)

func loadAppConfigFromFile(path string) (AppConfig, string, error) {
	configFile, err := FindConfigFile(path)
	if err != nil {
		return AppConfig{}, "", err
	}

	format, err := getConfigFormat(configFile)
	if err != nil {
		return AppConfig{}, "", err
	}

	parser, err := getConfigParser(format)
	if err != nil {
		return AppConfig{}, "", err
	}

	k := koanf.New(".")
	if err := k.Load(file.Provider(configFile), parser); err != nil {
		return AppConfig{}, "", fmt.Errorf("failed to load config file: %w", err)
	}

	configKeys := k.Keys()
	appConfigType := reflect.TypeOf(AppConfig{})

	if err := checkUnknownFields(appConfigType, configKeys, format); err != nil {
		return AppConfig{}, "", err
	}

	var appConfig AppConfig
	decoderConfig := &mapstructure.DecoderConfig{
		TagName: format,
		Result:  &appConfig,
		// This ensures that embedded structs with inline tags work properly
		Squash:     true,
		DecodeHook: portDecodeHook(),
	}

	unmarshalConf := koanf.UnmarshalConf{
		Tag:           format,
		DecoderConfig: decoderConfig,
	}

	if err := k.UnmarshalWithConf("", &appConfig, unmarshalConf); err != nil {
		return AppConfig{}, "", fmt.Errorf("failed to unmarshal config: %w", err)
	}

	return appConfig, format, nil
}
