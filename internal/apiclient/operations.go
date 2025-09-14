package apiclient

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/ameistad/haloy/internal/apitypes"
	"github.com/ameistad/haloy/internal/config"
	"github.com/ameistad/haloy/internal/constants"
	"github.com/ameistad/haloy/internal/logging"
	"github.com/ameistad/haloy/internal/ui"
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

// StreamLogs streams all logs from haloyd
func (c *APIClient) StreamLogs(ctx context.Context, logCh chan StreamLogEvent) {
	handler := func(data string) bool {
		var event StreamLogEvent
		var logEntry logging.LogEntry
		if err := json.Unmarshal([]byte(data), &logEntry); err != nil {
			event.Err = fmt.Errorf("failed to parse log entry: %w", err)
			logCh <- event
		}

		// Never stop streaming for general logs
		return false
	}
	c.stream(ctx, "logs", handler)
}

// Version retrieves the version information of haloyd and HAProxy
func (c *APIClient) Version(ctx context.Context) (*apitypes.VersionResponse, error) {
	var response apitypes.VersionResponse
	if err := c.get(ctx, "version", &response); err != nil {
		return nil, err
	}
	return &response, nil
}

func (c *APIClient) StreamHaloydInitLogs(ctx context.Context, onLogEntry func(logEntry logging.LogEntry), onError func(err error)) {
	handler := func(data string) bool {
		var logEntry logging.LogEntry
		if err := json.Unmarshal([]byte(data), &logEntry); err != nil {
			onError(fmt.Errorf("failed to parse log entry: %w", err))
			return false
		}

		onLogEntry(logEntry)

		// Stop streaming when haloyd init is complete
		return logEntry.IsHaloydInitComplete
	}
	c.stream(ctx, "logs", handler)
}

// displayDeploymentLogEntry formats and displays a deployment-specific log entry
func (c *APIClient) DisplayDeploymentLogEntry(entry logging.LogEntry) {
	message := entry.Message

	// Handle multi-line errors
	if errorStr := c.extractErrorField(entry); errorStr != "" {
		if strings.Contains(errorStr, "\n") {
			c.displayMultiLineError(entry.Level, message, errorStr)
			return
		} else {
			message = fmt.Sprintf("%s (error message: %s)", message, errorStr)
		}
	}

	c.displayMessage(message, entry)
}

// displayGeneralLogEntry formats and displays a general log entry
func (c *APIClient) displayGeneralLogEntry(entry logging.LogEntry) {
	message := entry.Message

	// Add deployment context for general logs
	if entry.DeploymentID != "" {
		message = fmt.Sprintf("[%s] %s", entry.DeploymentID, message)
	}
	if entry.AppName != "" && entry.AppName != entry.DeploymentID {
		message = fmt.Sprintf("[%s] %s", entry.AppName, message)
	}

	// Handle multi-line errors
	if errorStr := c.extractErrorField(entry); errorStr != "" {
		if strings.Contains(errorStr, "\n") {
			c.displayMultiLineError(entry.Level, message, errorStr)
			return
		} else {
			message = fmt.Sprintf("%s (error=%s)", message, errorStr)
		}
	}

	c.displayMessage(message, entry)
}

// extractErrorField extracts the error field from log entry if present
func (c *APIClient) extractErrorField(entry logging.LogEntry) string {
	if len(entry.Fields) > 0 {
		if errorValue, hasError := entry.Fields["error"]; hasError {
			return fmt.Sprintf("%v", errorValue)
		}
	}
	return ""
}

// displayMultiLineError displays multi-line errors with proper formatting
func (c *APIClient) displayMultiLineError(level, message, errorStr string) {
	switch strings.ToUpper(level) {
	case "ERROR":
		ui.Error("%s", message)
		scanner := bufio.NewScanner(strings.NewReader(errorStr))
		for scanner.Scan() {
			line := scanner.Text()
			if strings.TrimSpace(line) != "" {
				ui.Error("    %s", line)
			}
		}
	case "WARN":
		ui.Warn("%s", message)
		scanner := bufio.NewScanner(strings.NewReader(errorStr))
		for scanner.Scan() {
			line := scanner.Text()
			if strings.TrimSpace(line) != "" {
				ui.Warn("    %s", line)
			}
		}
	default:
		ui.Info("%s", message)
		scanner := bufio.NewScanner(strings.NewReader(errorStr))
		for scanner.Scan() {
			line := scanner.Text()
			if strings.TrimSpace(line) != "" {
				fmt.Printf("    %s\n", line)
			}
		}
	}
}

// displayMessage displays a log message with appropriate formatting based on level
func (c *APIClient) displayMessage(message string, entry logging.LogEntry) {
	isSuccess := entry.IsDeploymentSuccess
	domains := entry.Domains

	switch strings.ToUpper(entry.Level) {
	case "ERROR":
		ui.Error("%s", message)
	case "WARN":
		ui.Warn("%s", message)
	case "INFO":
		if isSuccess {
			if len(domains) > 0 {
				urls := make([]string, len(domains))
				for i, domain := range domains {
					urls[i] = fmt.Sprintf("https://%s", domain)
				}
				message = fmt.Sprintf("%s → %s", message, strings.Join(urls, ", "))
			}
			ui.Success("%s", message)
		} else {
			if len(domains) > 0 {
				message = fmt.Sprintf("%s (domains: %s)", message, strings.Join(domains, ", "))
			}
			ui.Info("%s", message)
		}
	case "DEBUG":
		ui.Debug("%s", message)
	default:
		fmt.Printf("%s\n", message)
	}
}
