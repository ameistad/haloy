package certificates

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
)

// KeyManager handles private key operations for the ACME client
type KeyManager struct {
	keyDir string
}

// NewKeyManager creates a new key manager
func NewKeyManager(keyDir string) (*KeyManager, error) {
	// Create key directory if it doesn't exist
	if err := os.MkdirAll(keyDir, 0700); err != nil {
		return nil, fmt.Errorf("failed to create key directory: %w", err)
	}

	return &KeyManager{
		keyDir: keyDir,
	}, nil
}

// LoadOrCreateKey loads an existing account key or creates a new one
func (km *KeyManager) LoadOrCreateKey(email string) (crypto.PrivateKey, error) {
	// Sanitize email for filename
	filename := sanitizeFilename(email) + ".key"
	keyPath := filepath.Join(km.keyDir, filename)

	// Check if key already exists
	if _, err := os.Stat(keyPath); err == nil {
		// Key exists, load it
		return km.loadKey(keyPath)
	}

	// Key doesn't exist, create a new one
	return km.createKey(keyPath)
}

// loadKey loads a private key from disk
func (km *KeyManager) loadKey(path string) (crypto.PrivateKey, error) {
	// Read key file
	keyBytes, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read key file: %w", err)
	}

	// Decode PEM
	keyBlock, _ := pem.Decode(keyBytes)
	if keyBlock == nil {
		return nil, fmt.Errorf("failed to decode PEM block")
	}

	// Parse private key
	switch keyBlock.Type {
	case "EC PRIVATE KEY":
		return x509.ParseECPrivateKey(keyBlock.Bytes)
	default:
		return nil, fmt.Errorf("unsupported key type: %s", keyBlock.Type)
	}
}

// createKey creates a new ECDSA private key and saves it to disk
func (km *KeyManager) createKey(path string) (crypto.PrivateKey, error) {
	// Generate new ECDSA key (P-256 for good balance of security and performance)
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("failed to generate private key: %w", err)
	}

	// Encode private key to PEM
	keyBytes, err := x509.MarshalECPrivateKey(privateKey)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal private key: %w", err)
	}

	// Create PEM block
	pemBlock := &pem.Block{
		Type:  "EC PRIVATE KEY",
		Bytes: keyBytes,
	}

	// Write key to file
	keyFile, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return nil, fmt.Errorf("failed to create key file: %w", err)
	}
	defer keyFile.Close()

	if err := pem.Encode(keyFile, pemBlock); err != nil {
		return nil, fmt.Errorf("failed to write key file: %w", err)
	}

	return privateKey, nil
}

func sanitizeFilename(email string) string {
	result := ""
	for _, c := range email {
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' {
			result += string(c)
		} else {
			result += "_"
		}
	}
	return result
}
