package manager

import (
	"bytes"
	"fmt"
	"strings"
	"text/template"

	"github.com/ameistad/haloy/internal/config"
	"github.com/ameistad/haloy/internal/embed"
)

func CreateHAProxyConfig(deployments []Deployment) (bytes.Buffer, error) {

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
						httpsFrontend += fmt.Sprintf("%shttp-request redirect code 301 location https://%s%%[path] if %s\n",
							indent, domain.Canonical, aliasACLName)

						httpFrontend += fmt.Sprintf("%sacl %s hdr(host) -i %s\n", indent, aliasACLName, alias)
						httpFrontend += fmt.Sprintf("%shttp-request redirect code 301 location https://%s%%[path] if %s\n",
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

// TODO: investigate options to use the running haproxy container to validate the config file.
