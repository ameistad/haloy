package haloyadm

import (
	"context"
	"path/filepath"
	"time"

	"filippo.io/age"
	"github.com/ameistad/haloy/internal/config"
	"github.com/ameistad/haloy/internal/constants"
	"github.com/ameistad/haloy/internal/db"
	"github.com/ameistad/haloy/internal/secrets"
	"github.com/ameistad/haloy/internal/ui"
	"github.com/joho/godotenv"
	"github.com/spf13/cobra"
)

const (
	secretsRollTimeout = 1 * time.Minute
)

func SecretsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "secrets",
		Short: "Manage secrets",
	}

	cmd.AddCommand(SecretsRollCmd())
	return cmd
}

func SecretsRollCmd() *cobra.Command {
	var devMode bool
	var debug bool

	cmd := &cobra.Command{
		Use:   "roll",
		Short: "Roll all secrets",
		Long:  "Roll all secrets by generating a new encryption key and re-encrypting all secrets.",
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			ctx, cancel := context.WithTimeout(context.Background(), secretsRollTimeout)
			defer cancel()

			configDir, err := config.ConfigDir()
			if err != nil {
				ui.Error("Failed to determine config directory: %v\n", err)
				return
			}

			dataDir, err := config.DataDir()
			if err != nil {
				ui.Error("Failed to determine data directory: %v\n", err)
				return
			}
			envFile := filepath.Join(configDir, ".env")
			env, err := godotenv.Read(envFile)
			if err != nil {
				ui.Error("Failed to read environment variables from %s: %v", envFile, err)
				return
			}
			oldEncryptionKey, ok := env[constants.EnvVarAgeIdentity]
			if !ok || oldEncryptionKey == "" {
				ui.Error("%s is not set in %s", constants.EnvVarAgeIdentity, envFile)
				return
			}
			oldIdentity, err := age.ParseX25519Identity(oldEncryptionKey)
			if err != nil {
				ui.Error("failed to parse age identity from %s environment variable: %v", constants.EnvVarAgeIdentity, err)
				return
			}

			newIdentity, err := age.GenerateX25519Identity()
			if err != nil {
				ui.Error("Failed to generate new encryption key: %v\n", err)
				return
			}

			database, err := db.New()
			if err != nil {
				ui.Error("Failed to connect to database: %v", err)
				return
			}

			secretsList, err := database.GetSecretsList()
			if err != nil {
				ui.Error("Failed to retrieve secrets: %v", err)
				return
			}

			var batchSecrets []db.SecretBatch
			for _, secret := range secretsList {
				decryptedValue, err := secrets.Decrypt(secret.Name, oldIdentity)
				if err != nil {
					ui.Error("Failed to decrypt secret %s: %v", secret.Name, err)
					return
				}
				newEncryptedValue, err := secrets.Encrypt(decryptedValue, newIdentity.Recipient())
				if err != nil {
					ui.Error("Failed to re-encrypt secret %s: %v", secret.Name, err)
					return
				}
				batchSecrets = append(batchSecrets, db.SecretBatch{
					Name:           secret.Name,
					EncryptedValue: newEncryptedValue,
					// CreatedAt and UpdatedAt will be set in SetSecretsBatch
				})
			}

			if err := database.SetSecretsBatch(batchSecrets); err != nil {
				ui.Error("Failed to update secrets: %v", err)
				return
			}

			env[constants.EnvVarAgeIdentity] = newIdentity.String()
			if err := godotenv.Write(env, envFile); err != nil {
				ui.Error("Failed to write environment variables to %s: %v", envFile, err)
				return
			}

			// Restart haloy-manager
			if err := stopContainer(ctx, config.ManagerLabelRole); err != nil {
				ui.Error("Failed to stop haloy-manager container: %v", err)
				return
			}
			if err := startHaloyManager(ctx, dataDir, configDir, false, false); err != nil {
				ui.Error("Failed to restart haloy-manager: %v", err)
				return
			}
			ui.Success("All secrets rolled successfully")
		},
	}
	cmd.Flags().BoolVar(&devMode, "dev", false, "Restart in development mode using the local haloy-manager image")
	cmd.Flags().BoolVar(&debug, "debug", false, "Restart haloy-manager in debug mode")
	return cmd
}
