package apiclient

import (
	"context"
	"fmt"

	"github.com/ameistad/haloy/internal/apitypes"
)

// TODO: move this logic into the haloy package

func (c *APIClient) AppStatus(ctx context.Context, appName string) (*apitypes.AppStatusResponse, error) {
	if appName == "" {
		return nil, fmt.Errorf("app name is required")
	}

	path := fmt.Sprintf("status/%s", appName)
	var response apitypes.AppStatusResponse
	if err := c.Get(ctx, path, &response); err != nil {
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
	if err := c.Post(ctx, path, nil, &response); err != nil {
		return nil, err
	}
	return &response, nil
}

// Version retrieves the version information of haloyd and HAProxy
func (c *APIClient) Version(ctx context.Context) (*apitypes.VersionResponse, error) {
	var response apitypes.VersionResponse
	if err := c.Get(ctx, "version", &response); err != nil {
		return nil, err
	}
	return &response, nil
}
