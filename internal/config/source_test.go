package config

import (
	"os"
	"path/filepath"
	"testing"
	// ...existing code...
)

func TestSource_Validate(t *testing.T) {
	// Create a temporary directory for the test
	tempDir := t.TempDir()
	dockerfilePath := filepath.Join(tempDir, "Dockerfile")
	buildContextPath := tempDir // Use the same temp dir as build context

	// Create a dummy Dockerfile
	if err := os.WriteFile(dockerfilePath, []byte("FROM scratch"), 0644); err != nil {
		t.Fatalf("Failed to create temp Dockerfile: %v", err)
	}

	tests := []struct {
		name    string
		source  Source
		wantErr bool
	}{
		{
			name: "valid dockerfile source",
			source: Source{
				// Use the paths to the temporary files/dirs created above
				Dockerfile: &DockerfileSource{Path: dockerfilePath, BuildContext: buildContextPath},
			},
			wantErr: false, // Now expects false as the file exists
		},
		// ...existing code...
		{
			name: "invalid dockerfile missing path",
			source: Source{
				// Use a non-existent path within the temp dir for a controlled failure
				Dockerfile: &DockerfileSource{BuildContext: buildContextPath, Path: filepath.Join(tempDir, "nonexistent")},
			},
			wantErr: true, // This should still fail validation (file not found)
		},
		{
			name: "invalid dockerfile missing buildContext",
			source: Source{
				// Use a non-existent path for build context
				Dockerfile: &DockerfileSource{Path: dockerfilePath, BuildContext: filepath.Join(tempDir, "nonexistent_context")},
			},
			wantErr: true, // This should fail validation (build context not found)
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// We are now actually checking file existence via the temp files
			err := tt.source.Validate()
			if (err != nil) != tt.wantErr {
				// Provide more context on failure
				t.Errorf("Source.Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
