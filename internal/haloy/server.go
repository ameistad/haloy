package haloy

import "github.com/spf13/cobra"

func ServerCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "server",
		Short: "Manage Haloy servers",
		Long:  "Add, remove, and manage connections to Haloy servers",
	}

	// cmd.AddCommand(ServerAddCmd())
	// cmd.AddCommand(ServerDeleteCmd())
	// cmd.AddCommand(ServerListCmd())

	return cmd
}

func ServerAddCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "add <url> <token>",
		Short: "Add a new Haloy server",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			// url, token := args[0], args[1]
			return nil
		},
	}
}
