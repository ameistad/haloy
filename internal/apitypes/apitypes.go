package apitypes

import (
	"github.com/ameistad/haloy/internal/config"
	"github.com/ameistad/haloy/internal/deploytypes"
)

type HealthResponse struct {
	Status  string `json:"status"`
	Version string `json:"version,omitempty"`
	Service string `json:"service"`
}

type DeployRequest struct {
	AppConfig    config.AppConfig `json:"app"`
	ConfigFormat string           `json:"configFormat,omitempty"`
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

type SecretListItemResponse struct {
	Name        string `json:"name"`
	DigestValue string `json:"digest_value"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

type SecretsListResponse struct {
	Secrets []SecretListItemResponse `json:"secrets"`
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
	Message string `json:"message,omitempty"`
}

type VersionResponse struct {
	Version        string `json:"manager"`
	HAProxyVersion string `json:"haproxy"`
}
