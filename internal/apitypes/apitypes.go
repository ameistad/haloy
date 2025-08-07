package apitypes

import (
	"github.com/ameistad/haloy/internal/config"
	"github.com/ameistad/haloy/internal/deploytypes"
	"github.com/ameistad/haloy/internal/storage"
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
	Targets []deploytypes.RollbackTarget `json:"targets"`
}

type SecretsListResponse struct {
	Secrets []storage.SecretAPIResponse `json:"secrets"`
}

type SetSecretRequest struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type AppStatusResponse struct {
	State        string
	DeploymentID string
	ContainerIDs []string
	// TODO: env vars, domains
}

type StopAppResponse struct {
	StoppedIDs []string `json:"stoppedIds"`
	RemovedIDs []string `json:"removed_ids,omitempty"`
}
