package embed

// HAProxyTemplateData holds all values your HAProxy template expects.
type HAProxyTemplateData struct {
	HTTPFrontend            string
	HTTPSFrontend           string
	HTTPSFrontendUseBackend string
	Backends                string
}
