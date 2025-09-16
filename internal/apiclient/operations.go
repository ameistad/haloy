package apiclient

import (
	"context"
	"fmt"
	"net/http"

	"github.com/ameistad/haloy/internal/apitypes"
	"github.com/ameistad/haloy/internal/config"
	"github.com/ameistad/haloy/internal/constants"
)

func (c *APIClient) Deploy(ctx context.Context, appConfig config.AppConfig, deploymentID, format string) (*apitypes.DeployResponse, error) {
	request := apitypes.DeployRequest{AppConfig: appConfig, DeploymentID: deploymentID, ConfigFormat: format}
	var response apitypes.DeployResponse
	err := c.post(ctx, "deploy", request, &response)
	return &response, err
}

func (c *APIClient) RollbackTargets(ctx context.Context, appName string) (*apitypes.RollbackTargetsResponse, error) {
	path := fmt.Sprintf("rollback/%s", appName)
	var response apitypes.RollbackTargetsResponse
	if err := c.get(ctx, path, &response); err != nil {
		return nil, err
	}
	return &response, nil
}

func (c *APIClient) Rollback(ctx context.Context, appName, targetDeploymentID, newDeploymentID string) (*apitypes.RollbackResponse, error) {
	path := fmt.Sprintf("rollback/%s/%s", appName, targetDeploymentID)
	request := apitypes.RollbackRequest{NewDeploymentID: newDeploymentID}
	var response apitypes.RollbackResponse
	if err := c.post(ctx, path, request, &response); err != nil {
		return nil, err
	}
	return &response, nil
}

func (c *APIClient) SecretsList(ctx context.Context) (*apitypes.SecretsListResponse, error) {
	var response apitypes.SecretsListResponse
	if err := c.get(ctx, "secrets", &response); err != nil {
		return nil, err
	}
	return &response, nil
}

func (c *APIClient) SetSecret(ctx context.Context, name, value string) error {
	request := apitypes.SetSecretRequest{
		Name:  name,
		Value: value,
	}
	if err := c.post(ctx, "secrets", request, nil); err != nil {
		return err
	}
	return nil
}

func (c *APIClient) DeleteSecret(ctx context.Context, name string) error {
	if name == "" {
		return fmt.Errorf("secret name is required")
	}

	path := fmt.Sprintf("secrets/%s", name)
	req, err := http.NewRequestWithContext(ctx, "DELETE", fmt.Sprintf("%s/v1/%s", c.baseURL, path), nil)
	if err != nil {
		return fmt.Errorf("failed to create DELETE request: %w", err)
	}
	c.setAuthHeader(req)

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send DELETE request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		if resp.StatusCode == http.StatusUnauthorized {
			return fmt.Errorf("authentication failed - check your %s", constants.EnvVarAPIToken)
		}
		return fmt.Errorf("DELETE request failed with status %d", resp.StatusCode)
	}

	return nil
}

func (c *APIClient) AppStatus(ctx context.Context, appName string) (*apitypes.AppStatusResponse, error) {
	if appName == "" {
		return nil, fmt.Errorf("app name is required")
	}

	path := fmt.Sprintf("status/%s", appName)
	var response apitypes.AppStatusResponse
	if err := c.get(ctx, path, &response); err != nil {
		return nil, err
	}
	return &response, nil
}

func (c *APIClient) StopApp(ctx context.Context, appName string, removeContainers bool) (*apitypes.StopAppResponse, error) {
	path := fmt.Sprintf("stop/%s", appName)

	// Add query parameter if removeContainers is true
	if removeContainers {
		path += "?remove-containers=true"
	}

	var response apitypes.StopAppResponse
	if err := c.post(ctx, path, nil, &response); err != nil {
		return nil, err
	}
	return &response, nil
}

// Version retrieves the version information of haloyd and HAProxy
func (c *APIClient) Version(ctx context.Context) (*apitypes.VersionResponse, error) {
	var response apitypes.VersionResponse
	if err := c.get(ctx, "version", &response); err != nil {
		return nil, err
	}
	return &response, nil
}
