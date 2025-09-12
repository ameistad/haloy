package ui

import (
	"bufio"
	"fmt"
	"strings"

	"github.com/ameistad/haloy/internal/logging"
)

// displayDeploymentLogEntry formats and displays log entries related to deployments
func DisplayDeploymentLogEntry(entry logging.LogEntry) {
	message := entry.Message

	// Handle multi-line errors
	if errorStr := extractErrorField(entry); errorStr != "" {
		if strings.Contains(errorStr, "\n") {
			displayMultiLineError(entry.Level, message, errorStr)
			return
		} else {
			message = fmt.Sprintf("%s (error message: %s)", message, errorStr)
		}
	}

	displayMessage(message, entry)
}

// displayGeneralLogEntry formats and displays a general log entry
func DisplayGeneralLogEntry(entry logging.LogEntry) {
	message := entry.Message

	// Add deployment context for general logs
	if entry.DeploymentID != "" {
		message = fmt.Sprintf("[%s] %s", entry.DeploymentID, message)
	}
	if entry.AppName != "" && entry.AppName != entry.DeploymentID {
		message = fmt.Sprintf("[%s] %s", entry.AppName, message)
	}

	// Handle multi-line errors
	if errorStr := extractErrorField(entry); errorStr != "" {
		if strings.Contains(errorStr, "\n") {
			displayMultiLineError(entry.Level, message, errorStr)
			return
		} else {
			message = fmt.Sprintf("%s (error=%s)", message, errorStr)
		}
	}

	displayMessage(message, entry)
}

func extractErrorField(entry logging.LogEntry) string {
	if len(entry.Fields) > 0 {
		if errorValue, hasError := entry.Fields["error"]; hasError {
			return fmt.Sprintf("%v", errorValue)
		}
	}
	return ""
}

func displayMultiLineError(level, message, errorStr string) {
	switch strings.ToUpper(level) {
	case "ERROR":
		Error("%s", message)
		scanner := bufio.NewScanner(strings.NewReader(errorStr))
		for scanner.Scan() {
			line := scanner.Text()
			if strings.TrimSpace(line) != "" {
				Error("    %s", line)
			}
		}
	case "WARN":
		Warn("%s", message)
		scanner := bufio.NewScanner(strings.NewReader(errorStr))
		for scanner.Scan() {
			line := scanner.Text()
			if strings.TrimSpace(line) != "" {
				Warn("    %s", line)
			}
		}
	default:
		Info("%s", message)
		scanner := bufio.NewScanner(strings.NewReader(errorStr))
		for scanner.Scan() {
			line := scanner.Text()
			if strings.TrimSpace(line) != "" {
				fmt.Printf("    %s\n", line)
			}
		}
	}
}

func displayMessage(message string, entry logging.LogEntry) {
	isSuccess := entry.IsDeploymentSuccess
	domains := entry.Domains

	switch strings.ToUpper(entry.Level) {
	case "ERROR":
		Error("%s", message)
	case "WARN":
		Warn("%s", message)
	case "INFO":
		if isSuccess {
			if len(domains) > 0 {
				urls := make([]string, len(domains))
				for i, domain := range domains {
					urls[i] = fmt.Sprintf("https://%s", domain)
				}
				message = fmt.Sprintf("%s → %s", message, strings.Join(urls, ", "))
			}
			Success("%s", message)
		} else {
			if len(domains) > 0 {
				message = fmt.Sprintf("%s (domains: %s)", message, strings.Join(domains, ", "))
			}
			Info("%s", message)
		}
	case "DEBUG":
		Debug("%s", message)
	default:
		fmt.Printf("%s\n", message)
	}
}
