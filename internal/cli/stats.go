package cli

import (
	"github.com/spf13/cobra"

	"github.com/rwrife/scratchpatch/internal/render"
	"github.com/rwrife/scratchpatch/internal/store"
)

func newStatsCommand() *cobra.Command {
	var noColor bool
	var asJSON bool

	cmd := &cobra.Command{
		Use:   "stats",
		Short: "Fun store metrics: footprint, oldest survivor, morgue rot, tag breakdown",
		Long: "Take the store's pulse. stats reports the little numbers that make the\n" +
			"whole scheme feel worth it:\n\n" +
			"  • living — how many scratches you're keeping and the bytes they hold.\n" +
			"  • oldest survivor — the scratch that has dodged the reaper longest.\n" +
			"  • morgue — recoverable soft-deleted bytes, and how many are already\n" +
			"    past the grace window (one `sp reap` from gone).\n" +
			"  • footprint — total bytes that passed through the store instead of\n" +
			"    rotting loose in /tmp (as far as the index can account for).\n" +
			"  • top tags — your most-used labels.\n\n" +
			"stats is read-only and derives everything from the index; it adds no\n" +
			"new counters and changes nothing.\n\n" +
			"Pass --json for a stable, machine-readable object (no color, no flavor):\n" +
			"`sp stats --json | jq '.totalBytes'`.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runStats(cmd, noColor, asJSON)
		},
	}

	cmd.Flags().BoolVar(&noColor, "no-color", false, "force plain output even on a TTY")
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit a JSON object instead of a report (for scripting)")

	return cmd
}

func runStats(cmd *cobra.Command, noColor, asJSON bool) error {
	st, err := store.Open()
	if err != nil {
		return err
	}

	stats, err := st.Stats()
	if err != nil {
		return err
	}

	out := cmd.OutOrStdout()
	data := toStatsData(stats)

	// --json is intentionally color- and personality-free, matching the other
	// scriptable paths.
	if asJSON {
		return render.StatsReportJSON(out, data)
	}

	color := !noColor && isTerminal(out)
	return render.StatsReport(out, data, color)
}

// toStatsData flattens the store's Stats into the render layer's plain view, so
// render never has to import the store package — the same adapter pattern
// doctor.go and reap.go use.
func toStatsData(s store.Stats) render.StatsData {
	tags := make([]render.StatsTag, len(s.Tags))
	for i, t := range s.Tags {
		tags[i] = render.StatsTag{Tag: t.Tag, Count: t.Count}
	}
	return render.StatsData{
		LiveCount:      s.LiveCount,
		LiveBytes:      s.LiveBytes,
		MorgueCount:    s.MorgueCount,
		MorgueBytes:    s.MorgueBytes,
		PurgeableCount: s.PurgeableCount,
		TotalBytes:     s.TotalBytes,
		OldestID:       s.OldestID,
		OldestName:     s.OldestName,
		OldestAge:      s.OldestAge,
		Tags:           tags,
		Grace:          s.Grace,
	}
}
