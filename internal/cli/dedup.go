package cli

import (
	"github.com/spf13/cobra"

	"github.com/rwrife/scratchpatch/internal/render"
	"github.com/rwrife/scratchpatch/internal/store"
)

func newDedupCommand() *cobra.Command {
	var noColor bool
	var asJSON bool
	var collapse bool

	cmd := &cobra.Command{
		Use:   "dedup",
		Short: "Find (and optionally collapse) byte-identical scratches",
		Long: "Throwaway files breed duplicates: the same log pasted three times, an\n" +
			"agent re-capturing identical output on every run, the same snippet\n" +
			"`sp new`'d across two projects. dedup hashes every live scratch's content\n" +
			"and groups the byte-identical copies into clusters.\n\n" +
			"By default dedup is strictly read-only: it reports the clusters, names the\n" +
			"oldest copy as canonical, and shows how many bytes the redundant copies\n" +
			"waste — it moves nothing.\n\n" +
			"Pass --collapse to send the redundant copies to the morgue, keeping each\n" +
			"cluster's canonical (oldest) member live. Like `sp rm`, this never\n" +
			"hard-deletes: collapsed copies are recoverable with `sp resurrect` until\n" +
			"the reaper purges them past the grace window.\n\n" +
			"dedup is content-equality only — distinct from `sp doctor` (index-vs-disk\n" +
			"drift) and `sp reap` (TTL-based). Pass --json for a stable, machine-\n" +
			"readable object (no color, no flavor): `sp dedup --json | jq '.clean'`.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDedup(cmd, noColor, asJSON, collapse)
		},
	}

	cmd.Flags().BoolVar(&noColor, "no-color", false, "force plain output even on a TTY")
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit a JSON object instead of a report (for scripting)")
	cmd.Flags().BoolVar(&collapse, "collapse", false, "move redundant copies to the morgue, keeping the oldest as canonical")

	return cmd
}

func runDedup(cmd *cobra.Command, noColor, asJSON, collapse bool) error {
	st, err := store.Open()
	if err != nil {
		return err
	}

	report, err := st.Dedup()
	if err != nil {
		return err
	}

	data := toDedupData(report)

	// --collapse moves the redundant copies to the morgue, then records the
	// outcome on the report so both the human and JSON views can confirm it.
	if collapse && !report.Clean() {
		res, err := st.Collapse(report)
		if err != nil {
			return err
		}
		data.Collapsed = &render.DedupCollapsedData{
			MovedIDs:       res.MovedIDs,
			ReclaimedBytes: res.ReclaimedBytes,
		}
	} else if collapse {
		// Nothing to collapse, but the user asked — report an empty outcome so
		// the message is "collapsed 0" rather than the read-only footer.
		data.Collapsed = &render.DedupCollapsedData{}
	}

	out := cmd.OutOrStdout()

	// --json is intentionally color-free and personality-free; never tint it,
	// regardless of TTY or --no-color, matching `sp ls --json`/`sp doctor --json`.
	if asJSON {
		return render.DedupReportJSON(out, data)
	}

	color := !noColor && isTerminal(out)
	return render.DedupReport(out, data, color)
}

// toDedupData flattens the store's DedupReport into the render layer's plain
// view, so render never has to import the store package — the same adapter
// pattern doctor.go/reap.go use.
func toDedupData(r store.DedupReport) render.DedupData {
	clusters := make([]render.DedupClusterData, 0, len(r.Clusters))
	for _, c := range r.Clusters {
		members := make([]render.DedupMemberData, 0, len(c.Members))
		for _, m := range c.Members {
			members = append(members, render.DedupMemberData{
				ID:        m.Scratch.ID,
				Name:      m.Scratch.Name,
				Size:      m.Scratch.Size,
				CreatedAt: m.Scratch.CreatedAt,
				Canonical: m.Canonical,
			})
		}
		clusters = append(clusters, render.DedupClusterData{
			Digest:      c.Digest,
			Members:     members,
			WastedBytes: c.WastedBytes,
		})
	}
	return render.DedupData{
		Clusters:     clusters,
		TotalWasted:  r.TotalWasted,
		ScannedCount: r.ScannedCount,
	}
}
