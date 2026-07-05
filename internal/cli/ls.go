package cli

import (
	"io"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/rwrife/scratchpatch/internal/index"
	"github.com/rwrife/scratchpatch/internal/render"
	"github.com/rwrife/scratchpatch/internal/secret"
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
			"A 🔑 next to a scratch's name means it tripped the secret tripwire — run\n" +
			"`sp scan <id>` to see the masked findings. Such scratches can't be\n" +
			"promoted into a repo without --allow-secrets.\n\n" +
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

	// Flag any live scratch that trips the secret tripwire so `sp ls` shows a
	// 🔑 next to it (and --json carries "secret": true). Scanning is best-effort:
	// a scratch whose content can't be read just goes unflagged rather than
	// failing the whole listing.
	markers := secretMarkers(st, scratches)

	if asJSON {
		return render.TableMarkedJSON(out, scratches, markers, now)
	}
	return render.TableMarked(out, scratches, markers, now, color)
}

// secretMarkers scans each scratch's content and returns the set of ids that
// tripped the secret tripwire, for `sp ls` to mark. It reads content directly
// and swallows per-scratch read errors: a listing should never fail because one
// file went missing, and doctor is the command that reports such drift. Returns
// nil when nothing tripped so the render layer can skip marking entirely.
func secretMarkers(st *store.Store, scratches []index.Scratch) map[string]bool {
	var markers map[string]bool
	for _, sc := range scratches {
		content, err := st.ReadContent(sc)
		if err != nil {
			continue
		}
		if secret.Tripped(content) {
			if markers == nil {
				markers = make(map[string]bool)
			}
			markers[sc.ID] = true
		}
	}
	return markers
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
