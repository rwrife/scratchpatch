package cli

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/rwrife/scratchpatch/internal/store"
	"github.com/rwrife/scratchpatch/internal/ttl"
)

// newFlags holds the parsed `sp new` options.
type newFlags struct {
	ttl    string
	ext    string
	tags   []string
	noEdit bool
}

// nonSlugChars matches runs of characters we don't want in an auto-generated
// scratch name, so a generated slug stays filesystem- and glance-friendly.
var nonSlugChars = regexp.MustCompile(`[^a-z0-9]+`)

func newNewCommand() *cobra.Command {
	var f newFlags

	cmd := &cobra.Command{
		Use:   "new [name]",
		Short: "Create a scratch and open it in $EDITOR",
		Long: "Create a new scratch file in the store, record its metadata, and open\n" +
			"it in $EDITOR. With no name, a dated slug is generated for you.\n\n" +
			"The scratch lives outside your repo and carries a TTL; once it expires\n" +
			"`sp reap` (M5) sweeps it into the recoverable morgue.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := ""
			if len(args) == 1 {
				name = args[0]
			}
			return runNew(cmd, name, f)
		},
	}

	// --ttl accepts friendly human durations via the M5 ttl engine: 30m, 2h,
	// 7d, 2w, or composites like 1w2d12h (Go-style 168h / 1h30m work too).
	// Omitting it applies the configured default (7d).
	cmd.Flags().StringVar(&f.ttl, "ttl", "", "scratch lifespan, e.g. 30m, 12h, 7d, 2w (default 7d)")
	cmd.Flags().StringVar(&f.ext, "ext", "", "file extension without a leading dot (default md)")
	cmd.Flags().StringArrayVar(&f.tags, "tag", nil, "tag to attach; may be repeated")
	cmd.Flags().BoolVar(&f.noEdit, "no-edit", false, "create the scratch without opening $EDITOR")

	return cmd
}

func runNew(cmd *cobra.Command, name string, f newFlags) error {
	st, err := store.Open()
	if err != nil {
		return err
	}

	// Parse the human TTL up front so a bad value fails before we create
	// anything. An empty flag means "use the configured default", which Create
	// applies when it sees a zero duration.
	var ttlDur time.Duration
	if s := strings.TrimSpace(f.ttl); s != "" {
		d, perr := ttl.Parse(s)
		if perr != nil {
			return fmt.Errorf("--ttl: %w", perr)
		}
		if d <= 0 {
			return fmt.Errorf("--ttl: %q is not a positive duration", f.ttl)
		}
		ttlDur = d
	}

	if strings.TrimSpace(name) == "" {
		name = generatedName(time.Now())
	}

	sc, path, err := st.Create(store.CreateOptions{
		Name: name,
		Ext:  f.ext,
		TTL:  ttlDur,
		Tags: f.tags,
	})
	if err != nil {
		return err
	}

	out := cmd.OutOrStdout()

	if !f.noEdit {
		if err := openInEditor(cmd, path); err != nil {
			// A missing/failed editor shouldn't lose the scratch — it's
			// already created. Tell the user where it is and how to open it.
			fmt.Fprintln(cmd.ErrOrStderr(), err)
			fmt.Fprintf(out, "scratch %s created at %s\n", sc.ID, path)
			return nil
		}
		// Refresh recorded size now that the user may have written content.
		if touched, terr := st.Touch(sc.ID); terr == nil {
			sc = touched
		}
	}

	fmt.Fprintf(out, "created scratch %s (%s) — %s\n", sc.ID, sc.Name, lifespanNote(sc.ExpiresAt, time.Now()))
	return nil
}

// lifespanNote renders a one-clause, tombstone-flavored reminder of when a
// freshly created scratch is due to be swept. It's deliberately terse so the
// confirmation line stays a single glanceable sentence, and it never fires for
// a scratch with no expiry (belt-and-suspenders — new scratches always get
// one). now is passed in so the wording is deterministic for tests.
func lifespanNote(expiresAt, now time.Time) string {
	if expiresAt.IsZero() {
		return "it'll live in the store until you reap it"
	}
	return "living on borrowed time, " + humanCountdown(expiresAt.Sub(now))
}

// humanCountdown phrases a time-until-expiry span for prose ("expires in ~7d"),
// or notes it's already due when the deadline has passed. It leans on the same
// compact day/hour/minute feel as the ls table without importing render.
func humanCountdown(d time.Duration) string {
	if d <= 0 {
		return "and already due for the reaper"
	}
	switch {
	case d >= 24*time.Hour:
		return fmt.Sprintf("expires in ~%dd", int(d/(24*time.Hour)))
	case d >= time.Hour:
		return fmt.Sprintf("expires in ~%dh", int(d/time.Hour))
	case d >= time.Minute:
		return fmt.Sprintf("expires in ~%dm", int(d/time.Minute))
	default:
		return "expires within the minute"
	}
}

// openInEditor launches $EDITOR on path, wiring it to the current stdio so an
// interactive editor works. It returns a friendly error when $EDITOR is unset
// so the caller can fall back gracefully.
func openInEditor(cmd *cobra.Command, path string) error {
	editor := strings.TrimSpace(os.Getenv("EDITOR"))
	if editor == "" {
		return errors.New("$EDITOR is not set — open the scratch manually, or set $EDITOR and re-run")
	}

	// Split EDITOR so values like "code --wait" or "vim -p" work.
	fields := strings.Fields(editor)
	bin := fields[0]
	editorArgs := append(fields[1:], path)

	ed := exec.Command(bin, editorArgs...)
	ed.Stdin = cmd.InOrStdin()
	ed.Stdout = cmd.OutOrStdout()
	ed.Stderr = cmd.ErrOrStderr()
	if err := ed.Run(); err != nil {
		return fmt.Errorf("editor %q exited with error: %w", editor, err)
	}
	return nil
}

// generatedName builds a dated slug like "scratch-2026-06-26-2041" for scratches
// created without an explicit name.
func generatedName(t time.Time) string {
	return "scratch-" + t.Format("2006-01-02-1504")
}

// slugify is reserved for normalizing user-supplied names into filesystem-safe
// slugs in later milestones; kept here so name handling lives in one place.
func slugify(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = nonSlugChars.ReplaceAllString(s, "-")
	return strings.Trim(s, "-")
}
