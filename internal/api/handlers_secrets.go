package api

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/ameistad/haloy/internal/apitypes"
	"github.com/ameistad/haloy/internal/constants"
	"github.com/ameistad/haloy/internal/secrets"
	"github.com/ameistad/haloy/internal/storage"
)

func (s *APIServer) handleSecretsList() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		db, err := storage.New(constants.DBPath)
		if err != nil {
			log.Printf("Error initializing database: %v", err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
		defer db.Close()
		secrets, err := db.GetSecretsList()
		if err != nil {
			log.Printf("Error fetching secrets: %v", err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}

		apiSecrets := make([]apitypes.SecretListItemResponse, len(secrets))
		for i, secret := range secrets {
			digest := md5.Sum([]byte(secret.EncryptedValue))
			digestStr := hex.EncodeToString(digest[:])
			apiSecrets[i] = apitypes.SecretListItemResponse{
				Name:        secret.Name,
				DigestValue: digestStr,
				CreatedAt:   secret.CreatedAt.Format(time.RFC3339),
				UpdatedAt:   secret.UpdatedAt.Format(time.RFC3339),
			}
		}
		response := apitypes.SecretsListResponse{Secrets: apiSecrets}
		if err := writeJSON(w, http.StatusAccepted, response); err != nil {
			log.Printf("Error writing JSON response: %v", err)
		}
	}
}

func (s *APIServer) handleDeleteSecret() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")
		if name == "" {
			http.Error(w, "Secret name is required", http.StatusBadRequest)
			return
		}
		db, err := storage.New(constants.DBPath)
		if err != nil {
			log.Printf("Error initializing database: %v", err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
		defer db.Close()

		if err := db.DeleteSecret(name); err != nil {
			log.Printf("Error deleting secret: %v", err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}

}

func (s *APIServer) handleSetSecret() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req apitypes.SetSecretRequest
		if err := decodeJSON(r.Body, &req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		if err := validateSetSecretRequest(req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		db, err := storage.New(constants.DBPath)
		if err != nil {
			log.Printf("Error initializing database: %v", err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
		defer db.Close()

		identity, err := secrets.GetAgeIdentity()
		if err != nil {
			log.Printf("Error getting age identity: %v", err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
		encryptedValue, err := secrets.Encrypt(req.Value, identity.Recipient())
		if err != nil {
			log.Printf("Error encrypting secret: %v", err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
		if err := db.SetSecret(req.Name, encryptedValue); err != nil {
			log.Printf("Error setting secret: %v", err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusNoContent)
	}
}
func validateSetSecretRequest(req apitypes.SetSecretRequest) error {
	if strings.TrimSpace(req.Name) == "" {
		return fmt.Errorf("secret name is required")
	}

	if strings.TrimSpace(req.Value) == "" {
		return fmt.Errorf("secret value is required")
	}

	if len(req.Name) > 255 {
		return fmt.Errorf("secret name too long (max 255 characters)")
	}

	// Check for valid characters (alphanumeric, underscore, hyphen)
	if !isValidSecretName(req.Name) {
		return fmt.Errorf("secret name can only contain letters, numbers, underscores, and hyphens")
	}

	if len(req.Value) > 10000 {
		return fmt.Errorf("secret value too long (max 10000 characters)")
	}

	return nil
}

func isValidSecretName(name string) bool {
	// Allow alphanumeric, underscore, hyphen, dot
	for _, char := range name {
		if !((char >= 'a' && char <= 'z') ||
			(char >= 'A' && char <= 'Z') ||
			(char >= '0' && char <= '9') ||
			char == '_' || char == '-' || char == '.') {
			return false
		}
	}
	return true
}
