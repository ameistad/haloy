package manager

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"text/template"
	"time"

	"github.com/ameistad/haloy/internal/config"
	"github.com/ameistad/haloy/internal/embed"
	"github.com/ameistad/haloy/internal/helpers"
	"github.com/ameistad/haloy/internal/logging"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
)

type HAProxyManager struct {
	cli         *client.Client
	logger      *logging.Logger
	configDir   string
	dryRun      bool
	updateMutex sync.Mutex // Mutex protects config writing and reload signaling
}

type HAProxyManagerConfig struct {
	Cli       *client.Client
	Logger    *logging.Logger
	ConfigDir string
	DryRun    bool
}

func NewHAProxyManager(config HAProxyManagerConfig) *HAProxyManager {
	return &HAProxyManager{
		cli:       config.Cli,
		logger:    config.Logger,
		configDir: config.ConfigDir,
		dryRun:    config.DryRun,
	}
}

// ApplyConfig generates, writes (if not dryRun), and reloads HAProxy config.
// This method is concurrency-safe due to the internal mutex.
func (hpm *HAProxyManager) ApplyConfig(ctx context.Context, deployments map[string]Deployment) error {
	hpm.logger.Info("HAProxyManager: Attempting to apply new configuration...")

	hpm.updateMutex.Lock()
	defer hpm.updateMutex.Unlock()

	// Generate Config (with certificate check)
	hpm.logger.Info("HAProxyManager: Generating new configuration...")
	configBuf, err := hpm.generateConfig(deployments)
	if err != nil {
		return fmt.Errorf("HAProxyManager: failed to generate config: %w", err)
	}

	if hpm.dryRun {
		hpm.logger.Info("HAProxyManager: DryRun - Skipping config write and reload.")
		hpm.logger.Info(configBuf.String())
		return nil
	}

	// Write Config File
	configPath := filepath.Join(hpm.configDir, config.HAProxyConfigFileName)
	hpm.logger.Info("HAProxyManager: Writing config")
	if err := os.WriteFile(configPath, configBuf.Bytes(), 0644); err != nil {
		return fmt.Errorf("HAProxyManager: failed to write config file %s: %w", configPath, err)
	}

	// Get HAProxy Container ID
	haproxyID, err := hpm.getContainerID(ctx)
	if err != nil {
		return fmt.Errorf("HAProxyManager: failed to find HAProxy container: %w", err)
	}
	if haproxyID == "" {
		hpm.logger.Warn("HAProxyManager: No HAProxy container found with label, cannot reload.")
		return nil // Not necessarily an error if HAProxy isn't running
	}

	// 4. Signal HAProxy Reload
	hpm.logger.Debug("HAProxyManager: Sending SIGUSR2 signal to HAProxy container...")
	err = hpm.cli.ContainerKill(ctx, haproxyID, "SIGUSR2")
	if err != nil {
		// Log error but potentially don't fail the whole update if signal fails? Or return error?
		// Let's return error for now.
		return fmt.Errorf("HAProxyManager: failed to send SIGUSR2 to HAProxy container %s: %w", helpers.SafeIDPrefix(haproxyID), err)
	}

	hpm.logger.Debug("HAProxyManager: Successfully signaled HAProxy for reload.")
	return nil
}

// generateConfig creates the HAProxy configuration content based on deployments.
// It checks for certificate existence before adding HTTPS bindings.
func (hpm *HAProxyManager) generateConfig(deployments map[string]Deployment) (bytes.Buffer, error) {
	var buf bytes.Buffer
	var httpFrontend string
	var httpsFrontend string
	var httpsFrontendUseBackend string
	var backends string
	const indent = "    "

	for appName, d := range deployments {
		backendName := appName
		var canonicalACLs []string

		// Skip processing if no domains are set for this deployment.
		if len(d.Labels.Domains) == 0 {
			continue
		}

		for _, domain := range d.Labels.Domains {
			if domain.Canonical != "" {
				canonicalKey := strings.ReplaceAll(domain.Canonical, ".", "_")
				canonicalACLName := fmt.Sprintf("%s_%s_canonical", backendName, canonicalKey)

				httpsFrontend += fmt.Sprintf("%sacl %s hdr(host) -i %s\n", indent, canonicalACLName, domain.Canonical)
				canonicalACLs = append(canonicalACLs, canonicalACLName)

				httpFrontend += fmt.Sprintf("%sacl %s hdr(host) -i %s\n", indent, canonicalACLName, domain.Canonical)
				// Redirect HTTP to HTTPS for the canonical domain but exclude ACME challenge.
				httpFrontend += fmt.Sprintf("%shttp-request redirect code 301 location https://%s%%[path] if %s !is_acme_challenge\n",
					indent, domain.Canonical, canonicalACLName)

				for _, alias := range domain.Aliases {
					if alias != "" {
						aliasKey := strings.ReplaceAll(alias, ".", "_")
						aliasACLName := fmt.Sprintf("%s_%s_alias", backendName, aliasKey)

						httpsFrontend += fmt.Sprintf("%sacl %s hdr(host) -i %s\n", indent, aliasACLName, alias)
						httpsFrontend += fmt.Sprintf("%shttp-request redirect code 301 location https://%s%%[path] if %s !is_acme_challenge\n",
							indent, domain.Canonical, aliasACLName)

						httpFrontend += fmt.Sprintf("%sacl %s hdr(host) -i %s\n", indent, aliasACLName, alias)
						httpFrontend += fmt.Sprintf("%shttp-request redirect code 301 location https://%s%%[path] if %s !is_acme_challenge\n",
							indent, domain.Canonical, aliasACLName)
					}
				}
			}
		}

		if len(canonicalACLs) > 0 {
			httpsFrontendUseBackend += fmt.Sprintf("%suse_backend %s if %s\n", indent, backendName, strings.Join(canonicalACLs, " or "))
		}
	}

	for _, d := range deployments {
		backendName := d.Labels.AppName
		backends += fmt.Sprintf("backend %s\n", backendName)
		for i, inst := range d.Instances {
			backends += fmt.Sprintf("%sserver app%d %s:%s check\n", indent, i+1, inst.IP, inst.Port)
		}
	}

	data, err := embed.TemplatesFS.ReadFile(fmt.Sprintf("templates/%s", config.HAProxyConfigFileName))
	if err != nil {
		return buf, fmt.Errorf("failed to read embedded file: %w", err)
	}

	tmpl, err := template.New("config").Parse(string(data))
	if err != nil {
		return buf, fmt.Errorf("failed to parse template: %w", err)
	}

	templateData := embed.HAProxyTemplateData{
		HTTPFrontend:            httpFrontend,
		HTTPSFrontend:           httpsFrontend,
		HTTPSFrontendUseBackend: httpsFrontendUseBackend,
		Backends:                backends,
	}

	if err := tmpl.Execute(&buf, templateData); err != nil {
		return buf, fmt.Errorf("failed to execute template: %w", err)
	}

	return buf, nil
}

func (hpm *HAProxyManager) getContainerID(ctx context.Context) (string, error) {
	// Configure retry parameters
	maxRetries := 30
	retryInterval := time.Second

	for retry := 0; retry < maxRetries; retry++ {
		// Check if context was canceled
		if ctx.Err() != nil {
			return "", fmt.Errorf("context canceled while waiting for HAProxy container: %w", ctx.Err())
		}

		// Set up filter for HAProxy container
		filtersArgs := filters.NewArgs()
		filtersArgs.Add("label", fmt.Sprintf("%s=%s", config.LabelRole, config.HAProxyLabelRole))
		filtersArgs.Add("status", "running") // Only consider running containers

		containers, err := hpm.cli.ContainerList(ctx, container.ListOptions{
			Filters: filtersArgs,
			Limit:   1, // We only expect one HAProxy container managed by haloy
		})
		if err != nil {
			return "", fmt.Errorf("failed to list containers with label %s=%s: %w",
				config.LabelRole, config.HAProxyLabelRole, err)
		}

		if len(containers) > 0 {
			// Found a running HAProxy container
			return containers[0].ID, nil
		}

		// No running container found yet - log on first attempt and halfway through
		if retry == 0 || retry == maxRetries/2 {
			hpm.logger.Info(fmt.Sprintf("HAProxyManager: Waiting for HAProxy container to be running. Attempt %d of %d", retry+1, maxRetries))
		}

		// Wait before retrying
		select {
		case <-ctx.Done():
			return "", fmt.Errorf("context canceled while waiting for HAProxy container: %w", ctx.Err())
		case <-time.After(retryInterval):
			// Continue to next retry
		}
	}

	return "", fmt.Errorf("timed out waiting for HAProxy container to be in running state after %d seconds",
		maxRetries)
}
