package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

// Version is the scratchpatch version. Overridable at build time via
// -ldflags "-X github.com/rwrife/scratchpatch/internal/cli.Version=vX.Y.Z".
var Version = "v0.0.0-dev"

func newVersionCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the scratchpatch version and tagline",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			_, err := fmt.Fprintf(cmd.OutOrStdout(), "scratchpatch %s\n%s\n", Version, Tagline)
			return err
		},
	}
}
