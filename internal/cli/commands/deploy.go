package commands

import (
	"fmt"

	"github.com/spf13/cobra"
)

func DeployAppCmd() *cobra.Command {
	deployAppCmd := &cobra.Command{
		Use:   "deploy <app-name>",
		Short: "Deploy an application",
		Long:  `Deploy a single application by name`,
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) != 1 {
				return fmt.Errorf("app deploy requires exactly one argument: the app name (e.g., 'haloy app deploy my-app')")
			}
			return nil
		},
		Run: func(cmd *cobra.Command, args []string) {
			appName := args[0]
			fmt.Print("Deploying app: ", appName, "\n")
		},
	}
	return deployAppCmd
}
