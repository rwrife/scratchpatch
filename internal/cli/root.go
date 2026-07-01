// Package cli wires up the scratchpatch command tree.
//
// M1 kept this tiny (root + `sp version`); M3 added the core create/view loop
// (`sp new`, `sp ls`). M4 fills in the lifecycle: `sp cat`, `sp open`, `sp rm`
// (soft-delete to the morgue), and `sp resurrect`, plus `sp ls --morgue`.
// M5 adds `sp reap`: sweep expired scratches to the morgue and hard-delete
// morgue items past the grace window (with `--dry-run`).
// M6 begins the polish pass with `sp doctor`: a read-only store health check
// that reconciles the index against what's actually on disk. M6 also adds
// `--json` output to `sp ls` for scripting and `sp completion` to generate
// bash/zsh/fish completion scripts.
package cli

import (
	"github.com/spf13/cobra"
)

// Tagline is the one-liner shown in help and `sp version`.
const Tagline = "git stash for the throwaway files you were never going to commit anyway."

// NewRootCommand builds the root `sp` command with all subcommands attached.
func NewRootCommand() *cobra.Command {
	root := &cobra.Command{
		Use:   "sp",
		Short: "scratchpatch — " + Tagline,
		Long: "scratchpatch (sp) gives every throwaway file a home with an expiration date.\n\n" +
			Tagline + "\n\n" +
			"Scratches live outside your repo, carry a TTL, and are swept into a\n" +
			"recoverable morgue before they're ever hard-deleted.",
		SilenceUsage:  true,
		SilenceErrors: true,
		// No default action yet; print help when called bare.
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}

	// We ship our own `sp completion` (completion.go) with scratchpatch-voiced
	// help, so suppress cobra's auto-generated default to avoid two commands
	// that do the same thing.
	root.CompletionOptions.DisableDefaultCmd = true

	root.AddCommand(
		newVersionCommand(),
		newNewCommand(),
		newLsCommand(),
		newCatCommand(),
		newOpenCommand(),
		newRmCommand(),
		newResurrectCommand(),
		newReapCommand(),
		newDoctorCommand(),
		newCompletionCommand(),
	)

	return root
}
