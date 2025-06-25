package api

import "github.com/ameistad/haloy/internal/config"

// DeployRequest is the expected JSON body for a POST /v1/deploy request.
type DeployRequest struct {
	AppConfig config.AppConfig `json:"app"`
}

// DeployResponse is the JSON response after starting a deployment.
type DeployResponse struct {
	DeploymentID string `json:"deploymentId"`
	Message      string `json:"message"`
}
