package embed

import "embed"

//go:embed init/*
var InitFS embed.FS

//go:embed templates/*
var TemplatesFS embed.FS
