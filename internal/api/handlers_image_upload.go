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
	"github.com/docker/docker/api/types/image"
)

// handleImageUpload handles uploading Docker image tar files
func (s *APIServer) handleImageUpload() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		fmt.Printf("=== IMAGE UPLOAD DEBUG START ===\n")

		// Parse multipart form (32MB max memory)
		if err := r.ParseMultipartForm(32 << 20); err != nil {
			fmt.Printf("Failed to parse multipart form: %v\n", err)
			http.Error(w, "Failed to parse multipart form", http.StatusBadRequest)
			return
		}
		fmt.Printf("Multipart form parsed successfully\n")

		// Get the uploaded file
		file, header, err := r.FormFile("image")
		if err != nil {
			fmt.Printf("Failed to get form file: %v\n", err)
			http.Error(w, "Missing 'image' file in form data", http.StatusBadRequest)
			return
		}
		defer file.Close()
		fmt.Printf("Got uploaded file: %s\n", header.Filename)

		// Validate file extension
		if !strings.HasSuffix(header.Filename, ".tar") {
			fmt.Printf("Invalid file extension: %s\n", header.Filename)
			http.Error(w, "File must be a .tar archive", http.StatusBadRequest)
			return
		}
		fmt.Printf("File extension validated\n")

		// Create temporary file, we defer delete it
		tempFile, err := os.CreateTemp("", "haloy-image-*.tar")
		if err != nil {
			fmt.Printf("Failed to create temp file: %v\n", err)
			http.Error(w, "Failed to create temporary file", http.StatusInternalServerError)
			return
		}
		defer func() {
			fmt.Printf("Cleaning up temp file: %s\n", tempFile.Name())
			os.Remove(tempFile.Name())
		}()
		defer tempFile.Close()
		fmt.Printf("Created temp file: %s\n", tempFile.Name())

		// Copy uploaded data to temp file
		bytesWritten, err := io.Copy(tempFile, file)
		if err != nil {
			fmt.Printf("Failed to copy file data: %v\n", err)
			http.Error(w, "Failed to save uploaded file", http.StatusInternalServerError)
			return
		}
		fmt.Printf("Copied %d bytes to temp file\n", bytesWritten)

		// Verify temp file exists and has content
		if stat, err := tempFile.Stat(); err != nil {
			fmt.Printf("Failed to stat temp file: %v\n", err)
		} else {
			fmt.Printf("Temp file size: %d bytes\n", stat.Size())
		}

		// Load the image into Docker
		ctx, cancel := context.WithTimeout(r.Context(), defaultContextTimeout)
		defer cancel()

		cli, err := docker.NewClient(ctx)
		if err != nil {
			fmt.Printf("Failed to create Docker client: %v\n", err)
			http.Error(w, "Failed to create Docker client", http.StatusInternalServerError)
			return
		}
		defer cli.Close()
		fmt.Printf("Docker client created successfully\n")

		fmt.Printf("About to load image from: %s\n", tempFile.Name())
		if err := docker.LoadImageFromTar(ctx, cli, tempFile.Name()); err != nil {
			fmt.Printf("Failed to load image: %v\n", err)
			http.Error(w, fmt.Sprintf("Failed to load image: %v", err), http.StatusInternalServerError)
			return
		}
		fmt.Printf("Image loaded successfully\n")

		// Verify image was loaded
		images, err := cli.ImageList(ctx, image.ListOptions{})
		if err != nil {
			fmt.Printf("Failed to list images: %v\n", err)
		} else {
			fmt.Printf("Total images after load: %d\n", len(images))
			for _, img := range images {
				fmt.Printf("  Image: %v\n", img.RepoTags)
			}
		}

		response := apitypes.ImageUploadResponse{
			Success: true,
			Message: fmt.Sprintf("Image loaded successfully from %s", header.Filename),
		}

		fmt.Printf("=== IMAGE UPLOAD DEBUG END ===\n")

		if err := encodeJSON(w, http.StatusAccepted, response); err != nil {
			http.Error(w, "Failed to encode response", http.StatusInternalServerError)
			return
		}
	}
}
