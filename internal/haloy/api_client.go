package haloy

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
	"github.com/ameistad/haloy/internal/logging"
	"github.com/ameistad/haloy/internal/ui"
)

// APIClient handles communication with the haloy API
type APIClient struct {
	client   *http.Client
	baseURL  string
	apiToken string
}

func NewAPIClient(serverURL string) *APIClient {
	token, err := config.LoadAPIToken()
	if err != nil {
		ui.Error("Failed to load API token: %v", err)
		ui.Info("Set HALOY_API_TOKEN environment variable or create a .env file")
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

func (c *APIClient) healthCheck(ctx context.Context) error {
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

func (c *APIClient) Get(ctx context.Context, path string, v any) error {
	if err := c.healthCheck(ctx); err != nil {
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
			return fmt.Errorf("authentication failed - check your HALOY_API_TOKEN")
		}
		return fmt.Errorf("GET request failed with status %d", resp.StatusCode)
	}

	if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
		return fmt.Errorf("failed to decode response: %w", err)
	}

	return nil
}

// ExecuteCommand sends a command to the API
func (c *APIClient) Post(ctx context.Context, path string, request, response interface{}) error {

	if err := c.healthCheck(ctx); err != nil {
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
			return fmt.Errorf("authentication failed - check your HALOY_API_TOKEN")
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

func (c *APIClient) Deploy(ctx context.Context, appConfig config.AppConfig) (*apitypes.DeployResponse, error) {
	request := apitypes.DeployRequest{AppConfig: appConfig}
	var response apitypes.DeployResponse
	err := c.Post(ctx, "deploy", request, &response)
	return &response, err
}

func (c *APIClient) RollbackTargets(ctx context.Context, appName string) (*apitypes.RollbackTargetsResponse, error) {
	path := fmt.Sprintf("rollback/%s", appName)
	var response apitypes.RollbackTargetsResponse
	if err := c.Get(ctx, path, &response); err != nil {
		return nil, err
	}
	return &response, nil
}

func (c *APIClient) Rollback(ctx context.Context, appName, targetDeploymentID string) (*apitypes.RollbackResponse, error) {
	path := fmt.Sprintf("rollback/%s/%s", appName, targetDeploymentID)
	var response apitypes.RollbackResponse
	if err := c.Post(ctx, path, nil, &response); err != nil {
		return nil, err
	}
	return &response, nil
}

func (c *APIClient) SecretsList(ctx context.Context) (*apitypes.SecretsListResponse, error) {
	var response apitypes.SecretsListResponse
	if err := c.Get(ctx, "secrets", &response); err != nil {
		return nil, err
	}
	return &response, nil
}

func (c *APIClient) SetSecret(ctx context.Context, name, value string) error {
	request := apitypes.SetSecretRequest{
		Name:  name,
		Value: value,
	}
	if err := c.Post(ctx, "secrets", request, nil); err != nil {
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
			return fmt.Errorf("authentication failed - check your HALOY_API_TOKEN")
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

// LogStreamer handles streaming logs from the haloy API for any command
type LogStreamer struct {
	client   *http.Client
	baseURL  string
	apiToken string
}

// NewLogStreamer creates a new log streamer
func NewLogStreamer(serverURL string) *LogStreamer {
	token, err := config.LoadAPIToken()
	if err != nil {
		ui.Warn("Failed to load API token for log streaming: %v", err)
	}
	return &LogStreamer{
		baseURL:  serverURL,
		apiToken: token,
		client: &http.Client{
			Timeout: 0, // No timeout for streaming
		},
	}
}

// StreamLogs connects to the SSE endpoint and displays logs for any command
func (s *LogStreamer) StreamLogs(ctx context.Context, command, deploymentID string) error {
	url := fmt.Sprintf("%s/v1/%s/%s/logs", s.baseURL, command, deploymentID)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return fmt.Errorf("failed to create SSE request: %w", err)
	}

	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Cache-Control", "no-cache")
	if s.apiToken != "" {
		req.Header.Set("Authorization", "Bearer "+s.apiToken)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to connect to log stream: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == http.StatusUnauthorized {
			return fmt.Errorf("authentication failed for log streaming - check your HALOY_API_TOKEN")
		}
		return fmt.Errorf("log stream returned status %d", resp.StatusCode)
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

			var logEntry logging.LogEntry
			if err := json.Unmarshal([]byte(data), &logEntry); err != nil {
				ui.Warn("Failed to parse log entry: %v", err)
				continue
			}

			// Format and display the log entry
			s.displayLogEntry(logEntry)

			// Check if operation is complete
			if logEntry.IsComplete {
				return nil
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("error reading log stream: %w", err)
	}

	return nil
}

// displayLogEntry formats and displays a log entry using the UI package
func (s *LogStreamer) displayLogEntry(entry logging.LogEntry) {
	message := entry.Message

	// Handle the error field specially for multi-line errors
	if len(entry.Fields) > 0 {
		if errorValue, hasError := entry.Fields["error"]; hasError {

			// Convert error to string
			errorStr := fmt.Sprintf("%v", errorValue)

			// Check if it's a multi-line error (contains newlines)
			if strings.Contains(errorStr, "\n") {
				// For multi-line errors, display them after the main message
				switch strings.ToUpper(entry.Level) {
				case "ERROR":
					ui.Error("%s", message)
					// Display the detailed error with proper indentation
					for _, line := range strings.Split(errorStr, "\n") {
						if strings.TrimSpace(line) != "" {
							ui.Error("    %s", line) // Indent error details
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
				return // Early return since we handled the error specially
			} else {
				// Single-line error, append to message
				message = fmt.Sprintf("%s (error=%s)", message, errorStr)
			}
		}
	}
	switch strings.ToUpper(entry.Level) {
	case "ERROR":
		ui.Error("%s", message)
	case "WARN":
		ui.Warn("%s", message)
	case "INFO":
		if entry.IsSuccess {
			if len(entry.Domains) > 0 {
				urls := make([]string, len(entry.Domains))
				for i, domain := range entry.Domains {
					urls[i] = fmt.Sprintf("https://%s", domain)
				}
				message = fmt.Sprintf("%s → %s", message, strings.Join(urls, ", "))
			}
			ui.Success("%s", message)
		} else {
			ui.Info("%s", message)
		}
	case "DEBUG":
		ui.Debug("%s", message)
	default:
		fmt.Printf("%s", message)
	}
}
