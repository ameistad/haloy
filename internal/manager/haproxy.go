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

	"github.com/ameistad/haloy/internal/config"
	"github.com/ameistad/haloy/internal/embed"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
	"github.com/sirupsen/logrus"
)

type HAProxyManager struct {
	dockerClient *client.Client
	logger       *logrus.Logger
	configDir    string
	dryRun       bool
	updateMutex  sync.Mutex // Mutex protects config writing and reload signaling
}

func NewHAProxyManager(cli *client.Client, logger *logrus.Logger, configDir, certDir string, dryRun bool) *HAProxyManager {
	return &HAProxyManager{
		dockerClient: cli,
		logger:       logger,
		configDir:    configDir,
		dryRun:       dryRun,
	}
}

// ApplyConfig generates, writes (if not dryRun), and reloads HAProxy config.
// This method is concurrency-safe due to the internal mutex.
func (hpm *HAProxyManager) ApplyConfig(ctx context.Context, deployments []Deployment) error {
	hpm.logger.Info("HAProxyManager: Attempting to apply new configuration...")

	hpm.updateMutex.Lock()
	defer hpm.updateMutex.Unlock()

	// 1. Generate Config (with certificate check)
	hpm.logger.Info("HAProxyManager: Generating new configuration...")
	configBuf, err := hpm.generateConfig(deployments)
	if err != nil {
		return fmt.Errorf("HAProxyManager: failed to generate config: %w", err)
	}

	if hpm.dryRun {
		hpm.logger.Infof("HAProxyManager: DryRun - Generated HAProxy config:\n%s", configBuf.String())
		hpm.logger.Info("HAProxyManager: DryRun - Skipping config write and reload.")
		return nil // Successful dry run
	}

	// 2. Write Config File
	configPath := filepath.Join(hpm.configDir, config.HAProxyConfigFileName)
	hpm.logger.Infof("HAProxyManager: Writing config to %s", configPath)
	if err := os.WriteFile(configPath, configBuf.Bytes(), 0644); err != nil {
		return fmt.Errorf("HAProxyManager: failed to write config file %s: %w", configPath, err)
	}

	// 3. Get HAProxy Container ID
	haproxyID, err := hpm.getContainerID(ctx)
	if err != nil {
		return fmt.Errorf("HAProxyManager: failed to find HAProxy container: %w", err)
	}
	if haproxyID == "" {
		hpm.logger.Warnf("HAProxyManager: No HAProxy container found with label %s=%s, cannot reload.", config.LabelRole, config.HAProxyLabelRole)
		return nil // Not necessarily an error if HAProxy isn't running
	}

	// 4. Signal HAProxy Reload
	hpm.logger.Infof("HAProxyManager: Sending SIGUSR2 signal to HAProxy container %s...", haproxyID[:12])
	err = hpm.dockerClient.ContainerKill(ctx, haproxyID, "SIGUSR2")
	if err != nil {
		// Log error but potentially don't fail the whole update if signal fails? Or return error?
		// Let's return error for now.
		return fmt.Errorf("HAProxyManager: failed to send SIGUSR2 to HAProxy container %s: %w", haproxyID[:12], err)
	}

	hpm.logger.Info("HAProxyManager: Successfully signaled HAProxy for reload.")
	hpm.logger.Info("HAProxyManager: Configuration apply process complete.")
	return nil
}

// generateConfig creates the HAProxy configuration content based on deployments.
// It checks for certificate existence before adding HTTPS bindings.
func (hpm *HAProxyManager) generateConfig(deployments []Deployment) (bytes.Buffer, error) {
	var buf bytes.Buffer
	var httpsFrontend string
	var httpFrontend string
	var backends string
	const indent = "    "

	for _, d := range deployments {
		backendName := d.Labels.AppName
		var canonicalACLs []string

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
			httpsFrontend += fmt.Sprintf("%suse_backend %s if %s\n", indent, backendName, strings.Join(canonicalACLs, " or "))
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

	templateData := struct {
		HTTPFrontend  string
		HTTPSFrontend string
		Backends      string
	}{
		HTTPFrontend:  httpFrontend,
		HTTPSFrontend: httpsFrontend,
		Backends:      backends,
	}

	if err := tmpl.Execute(&buf, templateData); err != nil {
		return buf, fmt.Errorf("failed to execute template: %w", err)
	}

	return buf, nil
}

func (hpm *HAProxyManager) getContainerID(ctx context.Context) (string, error) {
	filtersArgs := filters.NewArgs()
	filtersArgs.Add("label", fmt.Sprintf("%s=%s", config.LabelRole, config.HAProxyLabelRole))
	filtersArgs.Add("status", "running") // Only consider running containers

	containers, err := hpm.dockerClient.ContainerList(ctx, container.ListOptions{
		Filters: filtersArgs,
		Limit:   1, // We only expect one HAProxy container managed by haloy
	})
	if err != nil {
		return "", fmt.Errorf("failed to list containers with label %s=%s: %w",
			config.LabelRole, config.HAProxyLabelRole, err)
	}

	if len(containers) == 0 {
		return "", nil // No container found
	}

	return containers[0].ID, nil
}

// LEGACY
// func CreateHAProxyConfig(deployments []Deployment) (bytes.Buffer, error) {

// 	var buf bytes.Buffer
// 	var httpsFrontend string
// 	var httpFrontend string
// 	var backends string
// 	const indent = "    "

// 	for _, d := range deployments {
// 		backendName := d.Labels.AppName
// 		var canonicalACLs []string

// 		for _, domain := range d.Labels.Domains {
// 			if domain.Canonical != "" {
// 				canonicalKey := strings.ReplaceAll(domain.Canonical, ".", "_")
// 				canonicalACLName := fmt.Sprintf("%s_%s_canonical", backendName, canonicalKey)

// 				httpsFrontend += fmt.Sprintf("%sacl %s hdr(host) -i %s\n", indent, canonicalACLName, domain.Canonical)
// 				canonicalACLs = append(canonicalACLs, canonicalACLName)

// 				httpFrontend += fmt.Sprintf("%sacl %s hdr(host) -i %s\n", indent, canonicalACLName, domain.Canonical)
// 				// Redirect HTTP to HTTPS for the canonical domain but exclude ACME challenge.
// 				httpFrontend += fmt.Sprintf("%shttp-request redirect code 301 location https://%s%%[path] if %s !is_acme_challenge\n",
// 					indent, domain.Canonical, canonicalACLName)

// 				for _, alias := range domain.Aliases {
// 					if alias != "" {
// 						aliasKey := strings.ReplaceAll(alias, ".", "_")
// 						aliasACLName := fmt.Sprintf("%s_%s_alias", backendName, aliasKey)

// 						httpsFrontend += fmt.Sprintf("%sacl %s hdr(host) -i %s\n", indent, aliasACLName, alias)
// 						httpsFrontend += fmt.Sprintf("%shttp-request redirect code 301 location https://%s%%[path] if %s\n",
// 							indent, domain.Canonical, aliasACLName)

// 						httpFrontend += fmt.Sprintf("%sacl %s hdr(host) -i %s\n", indent, aliasACLName, alias)
// 						httpFrontend += fmt.Sprintf("%shttp-request redirect code 301 location https://%s%%[path] if %s\n",
// 							indent, domain.Canonical, aliasACLName)
// 					}
// 				}
// 			}
// 		}

// 		if len(canonicalACLs) > 0 {
// 			httpsFrontend += fmt.Sprintf("%suse_backend %s if %s\n", indent, backendName, strings.Join(canonicalACLs, " or "))
// 		}
// 	}

// 	for _, d := range deployments {
// 		backendName := d.Labels.AppName
// 		backends += fmt.Sprintf("backend %s\n", backendName)
// 		for i, inst := range d.Instances {
// 			backends += fmt.Sprintf("%sserver app%d %s:%s check\n", indent, i+1, inst.IP, inst.Port)
// 		}
// 	}

// 	data, err := embed.TemplatesFS.ReadFile(fmt.Sprintf("templates/%s", config.HAProxyConfigFileName))
// 	if err != nil {
// 		return buf, fmt.Errorf("failed to read embedded file: %w", err)
// 	}

// 	tmpl, err := template.New("config").Parse(string(data))
// 	if err != nil {
// 		return buf, fmt.Errorf("failed to parse template: %w", err)
// 	}

// 	templateData := struct {
// 		HTTPFrontend  string
// 		HTTPSFrontend string
// 		Backends      string
// 	}{
// 		HTTPFrontend:  httpFrontend,
// 		HTTPSFrontend: httpsFrontend,
// 		Backends:      backends,
// 	}

// 	if err := tmpl.Execute(&buf, templateData); err != nil {
// 		return buf, fmt.Errorf("failed to execute template: %w", err)
// 	}

// 	return buf, nil
// }

// TODO: investigate options to use the running haproxy container to validate the config file.
