package deploy

import "time"

const (
	// DefaultContextTimeout is the default timeout for context operations in the CLI.
	// This is used to ensure that operations do not hang indefinitely.
	DefaultContextTimeout = 120 * time.Second
)
