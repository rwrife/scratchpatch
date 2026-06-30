package cli

import (
	"io"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/rwrife/scratchpatch/internal/render"
	"github.com/rwrife/scratchpatch/internal/store"
)

func newLsCommand() *cobra.Command {
	var noColor bool
	var morgue bool
	var asJSON bool

	cmd := &cobra.Command{
		Use:   "ls",
		Short: "List live scratches as a colorized table",
		Long: "List the scratches currently in the store: id, name, age, time-to-\n" +
			"expiry, tags, and size. On a terminal the rows are color-coded by how\n" +
			"close each scratch is to expiry (green = fresh, amber = expiring soon,\n" +
			"red = expired). When piped or redirected, output is plain tab-separated\n" +
			"text with no color codes.\n\n" +
			"Pass --morgue to list soft-deleted scratches instead, showing how long\n" +
			"until each is purged for good.\n\n" +
			"Pass --json for a stable, machine-readable array (no color, no flavor)\n" +
			"suitable for scripting: `sp ls --json | jq '.[].id'`.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runLs(cmd, noColor, morgue, asJSON)
		},
	}

	cmd.Flags().BoolVar(&noColor, "no-color", false, "force plain output even on a TTY")
	cmd.Flags().BoolVar(&morgue, "morgue", false, "list soft-deleted scratches awaiting purge")
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit a JSON array instead of a table (for scripting)")

	return cmd
}

func runLs(cmd *cobra.Command, noColor, morgue, asJSON bool) error {
	st, err := store.Open()
	if err != nil {
		return err
	}

	out := cmd.OutOrStdout()
	// --json is intentionally color-free and personality-free; never tint it,
	// regardless of TTY or --no-color.
	color := !asJSON && !noColor && isTerminal(out)
	now := time.Now()

	if morgue {
		dead, err := st.ListMorgue()
		if err != nil {
			return err
		}
		rows := make([]render.MorgueRow, 0, len(dead))
		for _, sc := range dead {
			purgeAt, _ := st.PurgeAt(sc)
			rows = append(rows, render.MorgueRow{Scratch: sc, PurgeAt: purgeAt})
		}
		if asJSON {
			return render.MorgueTableJSON(out, rows, now)
		}
		return render.MorgueTable(out, rows, now, color)
	}

	scratches, err := st.ListLive()
	if err != nil {
		return err
	}

	if asJSON {
		return render.TableJSON(out, scratches, now)
	}
	return render.Table(out, scratches, now, color)
}

// isTerminal reports whether w is a character device (a TTY), which is our
// signal to emit color. Anything we can't prove is a terminal (pipes, files,
// buffers used in tests) is treated as not-a-TTY, so color never leaks into
// captured output. This is the one place the CLI inspects the output target;
// render itself only takes a bool.
func isTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}
