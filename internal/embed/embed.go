package embed

import "embed"

const (
	HAProxyConfigFileTemplate = "haproxy.cfg"
)

//go:embed data/*
var DataFS embed.FS

//go:embed templates/*
var TemplatesFS embed.FS
