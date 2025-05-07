package embed

import "embed"

const (
	HAProxyConfigFileTemplate = "haproxy.cfg"
	ConfigFileTemplate        = "apps.yml"
	ConfigFileTemplateTest    = "apps-with-test-app.yml"
)

//go:embed init/*
var InitFS embed.FS

//go:embed templates/*
var TemplatesFS embed.FS
