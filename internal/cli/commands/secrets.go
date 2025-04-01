package commands

import (
	"bytes"
	"crypto/md5"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"filippo.io/age"
	"github.com/ameistad/haloy/internal/config"
	"github.com/olekukonko/tablewriter"
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
		Use:   "set <key> <value>",
		Short: "Encrypt a plain-text value and store it under <key>",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			key := args[0]
			value := strings.Join(args[1:], " ")

			recipient, err := config.GetAgeRecipient()
			if err != nil {
				return err
			}

			// Encrypt the value.
			var rawBuffer bytes.Buffer
			encryptWriter, err := age.Encrypt(&rawBuffer, recipient)
			if err != nil {
				return fmt.Errorf("failed to initialize encryption: %w", err)
			}
			if _, err = io.WriteString(encryptWriter, value); err != nil {
				return fmt.Errorf("failed to write plain-text to encryption writer: %w", err)
			}
			if err := encryptWriter.Close(); err != nil {
				return fmt.Errorf("failed to close encryption writer: %w", err)
			}
			fullEncrypted := base64.StdEncoding.EncodeToString(rawBuffer.Bytes())

			newSecretRecord := config.SecretRecord{
				Encrypted: fullEncrypted,
				Date:      time.Now().Format(time.RFC3339),
			}

			secretRecords, err := config.LoadSecrets()
			if err != nil {
				return err
			}
			secretRecords[key] = newSecretRecord
			if err := config.SaveSecrets(secretRecords); err != nil {
				return err
			}
			fmt.Printf("Secret stored with key: %s\n", key)
			return nil
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

			// Set up tablewriter.
			table := tablewriter.NewWriter(os.Stdout)
			table.SetHeader([]string{"NAME", "DIGEST", "DATE"})

			for key, rec := range secrets {
				// Compute the digest from the encrypted value using MD5.
				digest := md5.Sum([]byte(rec.Encrypted))
				digestStr := hex.EncodeToString(digest[:])
				table.Append([]string{key, digestStr, rec.Date})
			}
			table.Render()
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
