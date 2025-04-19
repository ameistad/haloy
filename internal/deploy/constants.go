package deploy

import "time"

const (
	// DefaultDeployTimeout is the maximum time allowed for a deployment to complete
	// before it's considered failed. This includes time for building images, creating
	// containers, and waiting for health checks to pass.
	DefaultDeployTimeout = 120 * time.Second
)
