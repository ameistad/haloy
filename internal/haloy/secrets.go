package haloy

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/ameistad/haloy/internal/helpers"
	"github.com/ameistad/haloy/internal/ui"
	"github.com/spf13/cobra"
)

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

				date, err := helpers.FormatDateString(rec.Date)
				if err != nil {
					date = rec.Date // Fallback to raw date if formatting fails
				}
				rows = append(rows, []string{key, digestStr, date})
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
	cmd.AddCommand(SecretsSetCommand())
	cmd.AddCommand(SecretsListCommand())
	cmd.AddCommand(SecretsDeleteCommand())
	return cmd
}
