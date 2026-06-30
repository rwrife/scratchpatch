package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

// shellCompleters maps a shell name to the cobra generator that writes its
// completion script to the command's stdout. Keeping the set explicit (rather
// than leaning on cobra's default `completion` command) lets us route through
// cmd.OutOrStdout() for testability and phrase help in scratchpatch's voice.
var shellCompleters = map[string]func(*cobra.Command) error{
	"bash": func(c *cobra.Command) error { return c.Root().GenBashCompletionV2(c.OutOrStdout(), true) },
	"zsh":  func(c *cobra.Command) error { return c.Root().GenZshCompletion(c.OutOrStdout()) },
	"fish": func(c *cobra.Command) error { return c.Root().GenFishCompletion(c.OutOrStdout(), true) },
}

func newCompletionCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "completion <bash|zsh|fish>",
		Short: "Generate a shell completion script",
		Long: "Print a completion script for your shell to stdout. Pipe or source it to\n" +
			"get tab-completion for sp's commands and flags.\n\n" +
			"  bash:  source <(sp completion bash)\n" +
			"         # or persist it:\n" +
			"         sp completion bash > /etc/bash_completion.d/sp\n\n" +
			"  zsh:   sp completion zsh > \"${fpath[1]}/_sp\"\n" +
			"         # ensure `autoload -U compinit && compinit` runs in your .zshrc\n\n" +
			"  fish:  sp completion fish > ~/.config/fish/completions/sp.fish\n\n" +
			"The script is plain text on stdout — no color, no flavor — so it's safe\n" +
			"to redirect straight into a file.",
		Args:                  cobra.ExactArgs(1),
		ValidArgs:             []string{"bash", "zsh", "fish"},
		DisableFlagsInUseLine: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			shell := args[0]
			gen, ok := shellCompleters[shell]
			if !ok {
				return fmt.Errorf("unsupported shell %q: choose one of bash, zsh, fish", shell)
			}
			return gen(cmd)
		},
	}
}
