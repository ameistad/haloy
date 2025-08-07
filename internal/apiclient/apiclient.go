package apiclient

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/ameistad/haloy/internal/apitypes"
	"github.com/ameistad/haloy/internal/config"
	"github.com/ameistad/haloy/internal/constants"
	"github.com/ameistad/haloy/internal/logging"
	"github.com/ameistad/haloy/internal/ui"
)

// APIClient handles communication with the haloy API
type APIClient struct {
	client   *http.Client
	baseURL  string
	apiToken string
}

func New(serverURL string) *APIClient {
	token, err := config.LoadAPIToken()
	if err != nil {
		ui.Error("Failed to load API token: %v", err)
		ui.Info("Set %s environment variable or create a %s file", constants.EnvVarAPIToken, constants.ConfigEnvFileName)
		// Continue without token - let API calls fail with proper auth errors
	}
	return &APIClient{
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
		baseURL:  serverURL,
		apiToken: token,
	}
}

func (c *APIClient) setAuthHeader(req *http.Request) {
	if c.apiToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiToken)
	}
}

func (c *APIClient) HealthCheck(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, "GET", c.baseURL+"/health", nil)
	if err != nil {
		return fmt.Errorf("failed to create health check request: %w", err)
	}

	// Health endpoint doesn't require auth
	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("server not reachable: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("server returned status %d", resp.StatusCode)
	}

	return nil
}

func (c *APIClient) get(ctx context.Context, path string, v any) error {
	if err := c.HealthCheck(ctx); err != nil {
		return fmt.Errorf("server not available at %s: %w", c.baseURL, err)
	}

	url := fmt.Sprintf("%s/v1/%s", c.baseURL, path)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return fmt.Errorf("failed to create GET request: %w", err)
	}
	c.setAuthHeader(req)

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		if resp.StatusCode == http.StatusUnauthorized {
			return fmt.Errorf("authentication failed - check your %s", constants.EnvVarAPIToken)
		}
		return fmt.Errorf("GET request failed with status %d", resp.StatusCode)
	}

	if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
		return fmt.Errorf("failed to decode response: %w", err)
	}

	return nil
}

// ExecuteCommand sends a command to the API
func (c *APIClient) post(ctx context.Context, path string, request, response interface{}) error {

	if err := c.HealthCheck(ctx); err != nil {
		return fmt.Errorf("server not available at %s: %w", c.baseURL, err)
	}

	var jsonData []byte
	var err error

	// Handle nil request for endpoints that don't need request body
	if request != nil {
		jsonData, err = json.Marshal(request)
		if err != nil {
			return fmt.Errorf("failed to marshal request: %w", err)
		}
	}

	url := fmt.Sprintf("%s/v1/%s", c.baseURL, path)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	// Only set Content-Type if we have a request body
	if request != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	c.setAuthHeader(req)

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		if resp.StatusCode == http.StatusUnauthorized {
			return fmt.Errorf("authentication failed - check your %s", constants.EnvVarAPIToken)
		}
		return fmt.Errorf("POST request failed with status %d", resp.StatusCode)
	}

	if response != nil {
		if err := json.NewDecoder(resp.Body).Decode(response); err != nil {
			return fmt.Errorf("failed to decode response: %w", err)
		}
	}

	return nil
}

// Generic streaming method that handles any SSE endpoint
func (c *APIClient) stream(ctx context.Context, path string, handler func(data string) (bool, error)) error {
	streamingClient := &http.Client{Timeout: 0}

	url := fmt.Sprintf("%s/v1/%s", c.baseURL, path)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return fmt.Errorf("failed to create SSE request: %w", err)
	}

	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Cache-Control", "no-cache")
	c.setAuthHeader(req)

	resp, err := streamingClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to connect to stream: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == http.StatusUnauthorized {
			return fmt.Errorf("authentication failed for stream - check your %s", constants.EnvVarAPIToken)
		}
		return fmt.Errorf("stream returned status %d", resp.StatusCode)
	}

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		line := scanner.Text()

		// Skip empty lines and SSE comment lines
		if line == "" || strings.HasPrefix(line, ":") {
			continue
		}

		// Parse SSE data lines
		if strings.HasPrefix(line, "data: ") {
			data := strings.TrimPrefix(line, "data: ")

			// Call the handler function to process the data
			shouldStop, err := handler(data)
			if err != nil {
				ui.Warn("Failed to handle stream data: %v", err)
				continue
			}

			// If handler returns true, stop streaming
			if shouldStop {
				return nil
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("error reading stream: %w", err)
	}

	return nil
}

func (c *APIClient) Deploy(ctx context.Context, appConfig config.AppConfig) (*apitypes.DeployResponse, error) {
	request := apitypes.DeployRequest{AppConfig: appConfig}
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

func (c *APIClient) Rollback(ctx context.Context, appName, targetDeploymentID string) (*apitypes.RollbackResponse, error) {
	path := fmt.Sprintf("rollback/%s/%s", appName, targetDeploymentID)
	var response apitypes.RollbackResponse
	if err := c.post(ctx, path, nil, &response); err != nil {
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

// StreamDeploymentLogs streams logs for a specific deployment
func (c *APIClient) StreamDeploymentLogs(ctx context.Context, deploymentID string) error {
	path := fmt.Sprintf("deploy/%s/logs", deploymentID)

	return c.stream(ctx, path, func(data string) (bool, error) {
		var logEntry logging.LogEntry
		if err := json.Unmarshal([]byte(data), &logEntry); err != nil {
			return false, fmt.Errorf("failed to parse log entry: %w", err)
		}

		// Display deployment log (no deployment ID prefix needed)
		c.displayDeploymentLogEntry(logEntry)

		// Stop streaming when deployment is complete
		return logEntry.IsDeploymentComplete, nil
	})
}

// Version retrieves the version information of the manager and HAProxy
func (c *APIClient) Version(ctx context.Context) (*apitypes.VersionResponse, error) {
	var response apitypes.VersionResponse
	if err := c.get(ctx, "version", &response); err != nil {
		return nil, err
	}
	return &response, nil
}

// StreamLogs streams all manager logs
func (c *APIClient) StreamLogs(ctx context.Context) error {
	return c.stream(ctx, "logs", func(data string) (bool, error) {
		var logEntry logging.LogEntry
		if err := json.Unmarshal([]byte(data), &logEntry); err != nil {
			return false, fmt.Errorf("failed to parse log entry: %w", err)
		}

		c.displayGeneralLogEntry(logEntry)

		// Never stop streaming for general logs
		return false, nil
	})
}

func (c *APIClient) StreamManagerInitLogs(ctx context.Context) error {
	return c.stream(ctx, "logs", func(data string) (bool, error) {
		var logEntry logging.LogEntry
		if err := json.Unmarshal([]byte(data), &logEntry); err != nil {
			return false, fmt.Errorf("failed to parse log entry: %w", err)
		}

		c.displayGeneralLogEntry(logEntry)

		// Stop streaming when manager init is complete
		return logEntry.IsManagerInitComplete, nil
	})
}

// displayDeploymentLogEntry formats and displays a deployment-specific log entry
func (c *APIClient) displayDeploymentLogEntry(entry logging.LogEntry) {
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

// displayGeneralLogEntry formats and displays a general manager log entry
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
		for _, line := range strings.Split(errorStr, "\n") {
			if strings.TrimSpace(line) != "" {
				ui.Error("    %s", line)
			}
		}
	case "WARN":
		ui.Warn("%s", message)
		for _, line := range strings.Split(errorStr, "\n") {
			if strings.TrimSpace(line) != "" {
				ui.Warn("    %s", line)
			}
		}
	default:
		ui.Info("%s", message)
		for _, line := range strings.Split(errorStr, "\n") {
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
