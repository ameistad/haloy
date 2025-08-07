package constants

const (
	Version                  = "0.1.0"
	HAProxyVersion           = "3.2"
	ManagerContainerName     = "haloy-manager"
	HAProxyContainerName     = "haloy-haproxy"
	DockerNetwork            = "haloy-public"
	DefaultDeploymentsToKeep = 6
	DefaultHealthCheckPath   = "/"
	DefaultContainerPort     = "80"
	DefaultReplicas          = 1

	CertificatesHTTPProviderPort = "8080"
	APIServerPort                = "9999"
	DefaultAPIServerURL          = "http://localhost:9999" // Default URL for the haloy API server

	// Environment variables
	EnvVarAgeIdentity = "HALOY_ENCRYPTION_KEY"
	EnvVarAPIToken    = "HALOY_API_TOKEN"

	// Paths specific to the haloy manager which runs in a docker container. Important that they use consistent naming.
	HaloyConfigPath         = "/haloy-config"
	HAProxyConfigPath       = "/haproxy-config"
	CertificatesStoragePath = "/cert-storage"
	DBPath                  = "/db"

	// File names
	ManagerConfigFileName = "manager.yaml"
	ConfigEnvFileName     = ".env"
	HAProxyConfigFileName = "haproxy.cfg"
)
