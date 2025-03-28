package commands

import (
	"fmt"

	"github.com/ameistad/haloy/internal/config"
	"github.com/spf13/cobra"
)

func ListAppsCmd() *cobra.Command {
	listAppsCmd := &cobra.Command{
		Use:   "list",
		Short: "List all apps from config",
		RunE: func(cmd *cobra.Command, args []string) error {
			confFilePath, err := config.ConfigFilePath()
			if err != nil {
				return err
			}

			confFile, err := config.LoadAndValidateConfig(confFilePath)
			if err != nil {
				return fmt.Errorf("configuration error: %w", err)
			}

			fmt.Println("Apps in config:")
			for _, app := range confFile.Apps {
				fmt.Printf(" - %s\n", app.Name)
			}
			return nil
		},
	}
	return listAppsCmd
}
