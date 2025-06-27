package commands

import (
	"context"
	"strings"
	"time"

	"github.com/ameistad/haloy/internal/config"
	"github.com/ameistad/haloy/internal/ui"
)

// CommandExecutor handles common logic for executing API commands
type CommandExecutor struct {
	apiClient   *APIClient
	logStreamer *LogStreamer
}

// NewCommandExecutor creates a new command executor
func NewCommandExecutor(serverURL string) *CommandExecutor {
	return &CommandExecutor{
		apiClient:   NewAPIClient(serverURL),
		logStreamer: NewLogStreamer(serverURL),
	}
}

// ExecuteCommandWithLogs executes a command and optionally streams logs
func (e *CommandExecutor) ExecuteCommandWithLogs(
	ctx context.Context,
	command string,
	appConfig config.AppConfig,
	streamLogs bool,
) error {
	// Check if server is available
	ui.Info("🔍 Checking server availability...")
	if err := e.apiClient.IsServerAvailable(ctx); err != nil {
		if appConfig.Server == config.DefaultServer {
			ui.Error("Haloy API server is not running on %s", appConfig.Server)
			ui.Info("Start the haloy manager or specify a custom server URL with --server")
		} else {
			ui.Error("Server not available at %s: %v", appConfig.Server, err)
		}
		return err
	}

	ui.Success("Server is available")

	// Execute the command
	ui.Info("Executing %s...", command)

	var resp *APIResponse
	var err error

	switch command {
	case "deploy":
		resp, err = e.apiClient.Deploy(ctx, appConfig)
	case "rollback":
		resp, err = e.apiClient.Rollback(ctx, appConfig)
	case "status":
		resp, err = e.apiClient.Status(ctx, appConfig)
	default:
		// Generic command execution
		req := APIRequest{AppConfig: &appConfig}
		resp, err = e.apiClient.ExecuteCommand(ctx, command, req)
	}

	if err != nil {
		ui.Error("%s request failed: %v", strings.Title(command), err)
		return err
	}

	ui.Success("%s initiated successfully!", strings.Title(command))
	if resp.DeploymentID != "" {
		ui.Info("📦 Operation ID: %s", resp.DeploymentID)
	}
	if resp.Message != "" {
		ui.Info("💬 %s", resp.Message)
	}

	// Stream logs if requested and we have an operation ID
	if streamLogs && resp.DeploymentID != "" {
		ui.Info("📡 Connecting to %s logs...", command)

		// Give the operation a moment to start
		time.Sleep(1 * time.Second)

		logCtx, logCancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer logCancel()

		if err := e.logStreamer.StreamLogs(logCtx, command, resp.DeploymentID); err != nil {
			ui.Warn("Failed to stream logs: %v", err)
			ui.Info("You can check operation status manually")
		}
	} else if !streamLogs && resp.DeploymentID != "" {
		ui.Info("Log streaming disabled. Operation ID: %s", resp.DeploymentID)
	}

	return nil
}
