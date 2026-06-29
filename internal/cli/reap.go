package cli

import (
	"time"

	"github.com/spf13/cobra"

	"github.com/rwrife/scratchpatch/internal/render"
	"github.com/rwrife/scratchpatch/internal/store"
)

func newReapCommand() *cobra.Command {
	var noColor bool
	var dryRun bool

	cmd := &cobra.Command{
		Use:   "reap",
		Short: "Sweep expired scratches to the morgue and purge past-grace ones",
		Long: "Run the reaper. It does two things, in order, and never both to the\n" +
			"same scratch in one run:\n\n" +
			"  1. Expired live scratches are moved into the morgue (soft-deleted).\n" +
			"     Their grace clock starts now — they are NOT purged this run.\n" +
			"  2. Morgue scratches that have aged past the grace window (default\n" +
			"     3d) are hard-deleted for good. This is the only place\n" +
			"     scratchpatch ever destroys content.\n\n" +
			"Pass --dry-run to see exactly what would move and what would be\n" +
			"deleted without changing anything.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runReap(cmd, dryRun, noColor)
		},
	}

	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "show what would be swept/purged without changing anything")
	cmd.Flags().BoolVar(&noColor, "no-color", false, "force plain output even on a TTY")

	return cmd
}

func runReap(cmd *cobra.Command, dryRun, noColor bool) error {
	st, err := store.Open()
	if err != nil {
		return err
	}

	plan, err := st.Reap(time.Now(), dryRun)
	if err != nil {
		return err
	}

	out := cmd.OutOrStdout()
	color := !noColor && isTerminal(out)

	return render.ReapSummary(out, render.ReapResult{
		Swept:  plan.Morgued,
		Purged: plan.Purged,
		DryRun: plan.DryRun,
	}, color)
}
