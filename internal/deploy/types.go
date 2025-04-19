package deploy

// ContainerInfo represents essential information about a deployed container.
// It is used to track deployments and for operations like rolling back.
type ContainerInfo struct {
	ID           string
	DeploymentID string
}
