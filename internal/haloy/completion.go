package haloy

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// NewCompletionCmd creates a new completion command
func CompletionCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "completion [bash|zsh|fish|powershell]",
		Short: "Generate completion script",
		Args:  cobra.ExactArgs(1),
		Long: `To load completions:

Bash:
  $ source <(haloy completion bash)
  # Permanently:
  $ haloy completion bash > /etc/bash_completion.d/haloy  # Linux
  $ haloy completion bash > /usr/local/etc/bash_completion.d/haloy  # macOS

Zsh:
  $ echo "autoload -U compinit; compinit" >> ~/.zshrc
  $ source <(haloy completion zsh)

Fish:
  $ haloy completion fish | source

Powershell:
  PS> haloy completion powershell | Out-String | Invoke-Expression
`,
		RunE: func(cmd *cobra.Command, args []string) error {
			switch args[0] {
			case "bash":
				return cmd.Root().GenBashCompletion(os.Stdout)
			case "zsh":
				return cmd.Root().GenZshCompletion(os.Stdout)
			case "fish":
				return cmd.Root().GenFishCompletion(os.Stdout, true)
			case "powershell":
				return cmd.Root().GenPowerShellCompletionWithDesc(os.Stdout)
			default:
				return fmt.Errorf("unsupported shell type: %s", args[0])
			}
		},
	}

	return cmd
}
