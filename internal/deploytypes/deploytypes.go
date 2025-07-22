package deploytypes

import "github.com/ameistad/haloy/internal/config"

type RollbackTarget struct {
	DeploymentID string
	ImageID      string
	ImageTag     string
	IsRunning    bool // The image is live
	AppConfig    *config.AppConfig
}
