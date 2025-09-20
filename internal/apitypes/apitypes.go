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
	DeploymentID string           `json:"deploymentID"`
	ConfigFormat string           `json:"configFormat,omitempty"`
}

type RollbackRequest struct {
	TargetDeploymentID string `json:"targetDeploymentID"`
	NewDeploymentID    string `json:"newDeploymentID"`
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
	State        string          `json:"state"`
	DeploymentID string          `json:"deploymentId"`
	ContainerIDs []string        `json:"containerIds"`
	Domains      []config.Domain `json:"domains"` // TODO: env vars
}

type StopAppResponse struct {
	Message string `json:"message,omitempty"`
}

type VersionResponse struct {
	Version        string `json:"haloyd"`
	HAProxyVersion string `json:"haproxy"`
}
