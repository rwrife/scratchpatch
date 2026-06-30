package cli

import (
	"github.com/spf13/cobra"

	"github.com/rwrife/scratchpatch/internal/render"
	"github.com/rwrife/scratchpatch/internal/store"
)

func newDoctorCommand() *cobra.Command {
	var noColor bool

	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Check store health: orphaned files, missing content, footprint",
		Long: "Give the store a checkup. doctor reconciles the index against what's\n" +
			"actually on disk and reports any drift:\n\n" +
			"  • orphaned content — files in scratches/ or morgue/ with no index\n" +
			"    entry (bytes the store has forgotten how to describe).\n" +
			"  • missing content — index entries whose file is gone, so `sp cat`\n" +
			"    or `sp open` would fail on them.\n" +
			"  • footprint — how many live/morgue scratches you have and how much\n" +
			"    disk the content occupies.\n\n" +
			"doctor is read-only: it diagnoses but never moves or deletes anything.\n" +
			"Use `sp resurrect` to keep something, or remove stray files by hand.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDoctor(cmd, noColor)
		},
	}

	cmd.Flags().BoolVar(&noColor, "no-color", false, "force plain output even on a TTY")

	return cmd
}

func runDoctor(cmd *cobra.Command, noColor bool) error {
	st, err := store.Open()
	if err != nil {
		return err
	}

	diag, err := st.Doctor()
	if err != nil {
		return err
	}

	out := cmd.OutOrStdout()
	color := !noColor && isTerminal(out)

	return render.DoctorReport(out, toReportData(diag), color)
}

// toReportData flattens the store's Diagnosis into the render layer's plain
// view, so render never has to import the store package. This is the same
// adapter pattern reap.go uses to turn a ReapPlan into a render.ReapResult.
func toReportData(d store.Diagnosis) render.DoctorReportData {
	orphans := make([]render.DoctorOrphan, len(d.Orphans))
	for i, o := range d.Orphans {
		orphans[i] = render.DoctorOrphan{Path: o.Path, Area: o.Area, Size: o.Size}
	}
	missing := make([]render.DoctorMissing, len(d.Missing))
	for i, m := range d.Missing {
		missing[i] = render.DoctorMissing{
			ID:           m.Scratch.ID,
			Name:         m.Scratch.Name,
			ExpectedPath: m.ExpectedPath,
		}
	}
	return render.DoctorReportData{
		LiveCount:   d.LiveCount,
		MorgueCount: d.MorgueCount,
		TrackedSize: d.TrackedSize,
		OrphanSize:  d.OrphanSize,
		Orphans:     orphans,
		Missing:     missing,
	}
}
