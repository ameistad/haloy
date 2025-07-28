package haloy

import (
	"fmt"

	"github.com/ameistad/haloy/internal/version"
	"github.com/spf13/cobra"
)

// VersionCmd creates a new version command
func VersionCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "version",
		Short: "Print the current version of haloy",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("haloy %s\n", version.GetVersion())
		},
	}

	return cmd
}
