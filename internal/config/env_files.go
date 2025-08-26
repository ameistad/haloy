package config

import (
	"path/filepath"

	"github.com/ameistad/haloy/internal/constants"
	"github.com/joho/godotenv"
)

// LoadEnvFiles attempts to load .env files from various locations
// Does not return an error - just tries to load what it can find
func LoadEnvFiles() {
	_ = godotenv.Load(constants.ConfigEnvFileName)

	if configDir, err := ConfigDir(); err == nil {
		configEnvPath := filepath.Join(configDir, constants.ConfigEnvFileName)
		_ = godotenv.Load(configEnvPath)
	}
}
