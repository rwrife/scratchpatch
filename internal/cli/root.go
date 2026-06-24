// Package cli wires up the scratchpatch command tree.
//
// M1 keeps this intentionally tiny: a root command plus `sp version`.
// Store, index, and lifecycle commands arrive in later milestones.
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

	root.AddCommand(newVersionCommand())

	return root
}
