package api

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/ameistad/haloy/internal/apitypes"
	"github.com/ameistad/haloy/internal/docker"
)

// handleImageUpload handles uploading Docker image tar files
func (s *APIServer) handleImageUpload() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Parse multipart form (32MB max memory)
		if err := r.ParseMultipartForm(32 << 20); err != nil {
			http.Error(w, "Failed to parse multipart form", http.StatusBadRequest)
			return
		}

		// Get the uploaded file
		file, header, err := r.FormFile("image")
		if err != nil {
			http.Error(w, "Missing 'image' file in form data", http.StatusBadRequest)
			return
		}
		defer file.Close()

		// Validate file extension
		if !strings.HasSuffix(header.Filename, ".tar") {
			http.Error(w, "File must be a .tar archive", http.StatusBadRequest)
			return
		}

		// Create temporary file, we defer delete it
		tempFile, err := os.CreateTemp("", "haloy-image-*.tar")
		if err != nil {
			http.Error(w, "Failed to create temporary file", http.StatusInternalServerError)
			return
		}
		defer os.Remove(tempFile.Name())
		defer tempFile.Close()

		// Copy uploaded data to temp file
		if _, err := io.Copy(tempFile, file); err != nil {
			http.Error(w, "Failed to save uploaded file", http.StatusInternalServerError)
			return
		}

		// Load the image into Docker
		ctx, cancel := context.WithTimeout(r.Context(), defaultContextTimeout)
		defer cancel()

		cli, err := docker.NewClient(ctx)
		if err != nil {
			http.Error(w, "Failed to create Docker client", http.StatusInternalServerError)
			return
		}
		defer cli.Close()

		if err := docker.LoadImageFromTar(ctx, cli, tempFile.Name()); err != nil {
			http.Error(w, fmt.Sprintf("Failed to load image: %v", err), http.StatusInternalServerError)
			return
		}

		response := apitypes.ImageUploadResponse{
			Success: true,
			Message: fmt.Sprintf("Image loaded successfully from %s", header.Filename),
		}

		if err := encodeJSON(w, http.StatusAccepted, response); err != nil {
			http.Error(w, "Failed to encode response", http.StatusInternalServerError)
			return
		}
	}
}
