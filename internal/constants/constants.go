package constants

const (
	Version                  = "0.1.7"
	HAProxyVersion           = "3.2"
	ManagerContainerName     = "haloy-manager"
	HAProxyContainerName     = "haloy-haproxy"
	DockerNetwork            = "haloy-public"
	DefaultDeploymentsToKeep = 6
	DefaultHealthCheckPath   = "/"
	DefaultContainerPort     = "80"
	DefaultReplicas          = 1
	HAProxyConfigFileName    = "haproxy.cfg"

	// Paths specific to the haloy manager which runs in a docker container. Important that they use consistent naming.
	HaloyConfigPath         = "/haloy-config"
	HAProxyConfigPath       = "/haproxy-config"
	CertificatesStoragePath = "/cert-storage"
	DBPath                  = "/db"

	CertificatesHTTPProviderPort = "8080"
	APIServerPort                = "9999"
	DefaultAPIServerURL          = "http://localhost:9999" // Default URL for the haloy API server

	// Environment variables
	EnvVarAgeIdentity = "HALOY_ENCRYPTION_KEY"

	// File names
	ManagerConfigFileName = "manager.yaml"
)
