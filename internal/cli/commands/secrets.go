package commands

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"filippo.io/age"
	"github.com/ameistad/haloy/internal/config"
	"github.com/ameistad/haloy/internal/ui"
	"github.com/spf13/cobra"
)

// SecretsInitCommand creates a command to initialize the secrets system by generating the age identity.
func SecretsInitCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize secrets by generating an age identity",
		RunE: func(cmd *cobra.Command, args []string) error {
			identity, err := age.GenerateX25519Identity()
			if err != nil {
				return fmt.Errorf("failed to generate age identity: %w", err)
			}
			configDir, err := config.ConfigDirPath()
			if err != nil {
				return fmt.Errorf("failed to get config directory: %w", err)
			}
			identityPath := filepath.Join(configDir, config.IdentityFileName)
			// Create the identity file with restricted permissions (0600 - read/write for owner only)
			f, err := os.OpenFile(identityPath, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0600)
			if err != nil {
				return fmt.Errorf("failed to create identity file: %w", err)
			}
			defer f.Close()
			if _, err := f.WriteString(identity.String()); err != nil {
				return fmt.Errorf("failed to write identity to file: %w", err)
			}
			publicKey := identity.Recipient().String()
			fmt.Printf("Age identity generated and saved to %s\n", identityPath)
			fmt.Printf("Public key: %s\n", publicKey)
			return nil
		},
	}
	return cmd
}

// SecretsSetCommand encrypts a plain-text value and stores it under the provided key.
func SecretsSetCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "set <key> <value>",
		Short:   "Encrypt a plain-text value and store it under <key>",
		Example: "  haloy secrets set MY_SECRET supersecretvalue\n  haloy secrets set DB_PASSWORD 'p@ssw0rd!'",
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) < 2 {
				ui.Error("Error: You must provide a <key> and a <value> to store a secret.\n")
				ui.Info("%s", cmd.UsageString())
				return fmt.Errorf("requires at least 2 arg(s), only received %d", len(args))
			}
			return nil
		},
		Run: func(cmd *cobra.Command, args []string) {
			key := args[0]
			value := strings.Join(args[1:], " ")

			recipient, err := config.GetAgeRecipient()
			if err != nil {
				ui.Error("Failed to get age recipient: %v", err)
				return
			}

			encryptedValue, err := config.EncryptSecret(value, recipient)
			if err != nil {
				ui.Error("Failed to encrypt secret: %v", err)
				return
			}

			newSecretRecord := config.SecretRecord{
				Encrypted: encryptedValue,
				Date:      time.Now().Format(time.RFC3339),
			}

			secretRecords, err := config.LoadSecrets()
			if err != nil {
				ui.Error("Failed to load secrets: %v", err)
				return
			}
			secretRecords[key] = newSecretRecord
			if err := config.SaveSecrets(secretRecords); err != nil {
				ui.Error("Failed to save secret: %v", err)
				return
			}
			ui.Success("Secret stored successfully under key: %s", key)
		},
	}
	return cmd
}

// SecretsListCommand lists all stored secrets in a table.
func SecretsListCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all stored secrets",
		RunE: func(cmd *cobra.Command, args []string) error {
			secrets, err := config.LoadSecrets()
			if err != nil {
				return err
			}
			if len(secrets) == 0 {
				fmt.Println("No secrets stored.")
				return nil
			}

			headers := []string{"NAME", "DIGEST", "DATE"}
			rows := make([][]string, 0, len(secrets))
			for key, rec := range secrets {
				// Compute the digest from the encrypted value using MD5.
				digest := md5.Sum([]byte(rec.Encrypted))
				digestStr := hex.EncodeToString(digest[:])
				rows = append(rows, []string{key, digestStr, rec.Date})
			}

			ui.Table(headers, rows)
			return nil
		},
	}
	return cmd
}

func SecretsDeleteCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "delete <key>",
		Short: "Delete a stored secret by key",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			key := args[0]

			secretRecords, err := config.LoadSecrets()
			if err != nil {
				return err
			}
			if _, exists := secretRecords[key]; !exists {
				fmt.Printf("No secret found with key: %s\n", key)
				return nil
			}
			delete(secretRecords, key)
			if err := config.SaveSecrets(secretRecords); err != nil {
				return err
			}
			fmt.Printf("Secret deleted with key: %s\n", key)
			return nil
		},
	}
	return cmd
}

// SecretsCommand creates the parent secrets command with its subcommands.
func SecretsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "secrets",
		Short: "Manage secrets using age encryption",
	}
	cmd.AddCommand(SecretsInitCommand())
	cmd.AddCommand(SecretsSetCommand())
	cmd.AddCommand(SecretsListCommand())
	cmd.AddCommand(SecretsDeleteCommand())
	return cmd
}
