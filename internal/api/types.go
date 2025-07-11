package api

import (
	"github.com/ameistad/haloy/internal/config"
	"github.com/ameistad/haloy/internal/deploy"
)

type HealthResponse struct {
	Status  string `json:"status"`
	Version string `json:"version,omitempty"`
	Service string `json:"service"`
}

type DeployRequest struct {
	AppConfig config.AppConfig `json:"app"`
}

type DeployResponse struct {
	DeploymentID string `json:"deploymentId"`
}

type RollbackResponse struct {
	DeploymentID string `json:"deploymentId"`
}

type RollbackTargetsResponse struct {
	Targets []deploy.RollbackTarget `json:"targets"`
}
