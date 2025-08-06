package secrets

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"os"

	"filippo.io/age"
	"github.com/ameistad/haloy/internal/constants"
)

func GetAgeIdentity() (*age.X25519Identity, error) {
	identityStr := os.Getenv(constants.EnvVarAgeIdentity)
	if identityStr == "" {
		return nil, fmt.Errorf("environment variable %s is not set", constants.EnvVarAgeIdentity)
	}
	identity, err := age.ParseX25519Identity(identityStr)
	if err != nil {
		return nil, fmt.Errorf("failed to parse age identity from %s environment variable: %w", constants.EnvVarAgeIdentity, err)
	}
	return identity, nil
}

// EncryptSecret encrypts a plain-text value using the provided age recipient.
// It returns the encrypted value as a base64-encoded string for storage.
func Encrypt(value string, recipient age.Recipient) (string, error) {
	var rawBuffer bytes.Buffer
	encryptWriter, err := age.Encrypt(&rawBuffer, recipient)
	if err != nil {
		return "", fmt.Errorf("failed to initialize encryptor: %w", err)
	}
	if _, err = io.WriteString(encryptWriter, value); err != nil {
		return "", fmt.Errorf("failed to write value to encryption writer: %w", err)
	}
	if err := encryptWriter.Close(); err != nil {
		return "", fmt.Errorf("failed to close encryption writer: %w", err)
	}
	encodedValue := base64.StdEncoding.EncodeToString(rawBuffer.Bytes())
	return encodedValue, nil
}

// DecryptSecret decrypts a base64-encoded secret using the provided age identity.
// It returns the decrypted secret as a string.
func Decrypt(secret string, identity age.Identity) (string, error) {
	encryptedBytes, err := base64.StdEncoding.DecodeString(secret)
	if err != nil {
		return "", fmt.Errorf("failed to decode base64 secret: %w", err)
	}

	decryptReader, err := age.Decrypt(bytes.NewReader(encryptedBytes), identity)
	if err != nil {
		return "", fmt.Errorf("failed to decrypt value: %w", err)
	}

	var decryptedBuf bytes.Buffer
	if _, err := io.Copy(&decryptedBuf, decryptReader); err != nil {
		return "", fmt.Errorf("failed to read decrypted value: %w", err)
	}

	return decryptedBuf.String(), nil
}
