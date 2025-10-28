package deploytypes

import "github.com/ameistad/haloy/internal/config"

type RollbackTarget struct {
	DeploymentID string
	ImageID      string
	ImageRef     string
	IsRunning    bool // The image is live
	RawAppConfig *config.AppConfig
}
