package cli

import (
	"errors"
	"fmt"
	"io"
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
	ttl        string
	ext        string
	tags       []string
	noEdit     bool
	stdin      bool
	content    string
	fromFile   string
	contentSet bool
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

	// Headless capture flags (issue #27): seed content programmatically instead
	// of shelling out to $EDITOR. Any of these suppresses the editor so
	// scratchpatch can act as a sink for piped output, generated snippets, or
	// AI-agent temp files. Interactive `sp new` is unchanged when none are set.
	cmd.Flags().BoolVar(&f.stdin, "stdin", false, "read scratch content from standard input (no $EDITOR)")
	cmd.Flags().StringVar(&f.content, "content", "", "use this string as the scratch content (no $EDITOR)")
	cmd.Flags().StringVar(&f.fromFile, "from-file", "", "seed the scratch from an existing file (no $EDITOR)")

	cmd.PreRunE = func(c *cobra.Command, _ []string) error {
		f.contentSet = c.Flags().Changed("content")
		return nil
	}

	return cmd
}

// captureContent gathers headless content from --stdin/--content/--from-file.
// It returns the bytes, whether a headless source was requested at all, and an
// error for conflicting sources or read failures. Exactly one source may be
// used per invocation; --content "" is a deliberate empty scratch.
func captureContent(cmd *cobra.Command, f newFlags) (data []byte, headless bool, err error) {
	sources := 0
	if f.stdin {
		sources++
	}
	if f.contentSet {
		sources++
	}
	if strings.TrimSpace(f.fromFile) != "" {
		sources++
	}
	if sources == 0 {
		return nil, false, nil
	}
	if sources > 1 {
		return nil, true, errors.New("choose only one of --stdin, --content, or --from-file")
	}

	switch {
	case f.stdin:
		in := cmd.InOrStdin()
		// Guard against a hang when --stdin is used on a bare TTY with nothing
		// piped in: refuse rather than block the terminal forever.
		if file, ok := in.(*os.File); ok && isTerminal(file) {
			return nil, true, errors.New("--stdin expects piped input; nothing is attached to stdin")
		}
		b, rerr := io.ReadAll(in)
		if rerr != nil {
			return nil, true, fmt.Errorf("read stdin: %w", rerr)
		}
		return b, true, nil
	case f.contentSet:
		return []byte(f.content), true, nil
	default:
		b, rerr := os.ReadFile(f.fromFile)
		if rerr != nil {
			return nil, true, fmt.Errorf("read --from-file: %w", rerr)
		}
		return b, true, nil
	}
}

func runNew(cmd *cobra.Command, name string, f newFlags) error {
	st, err := store.Open()
	if err != nil {
		return err
	}

	// Gather any headless content up front so a bad source (conflicting flags,
	// unreadable file, TTY with no pipe) fails before we create a scratch.
	captured, headless, err := captureContent(cmd, f)
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

	// Headless capture path (issue #27): seed content from stdin/--content/
	// --from-file, skip $EDITOR entirely, and refresh size. Content is written
	// to disk, so `sp ls`'s live secret scan flags piped-in credentials (🔑)
	// and `sp promote` guards them, exactly like editor-created scratches.
	if headless {
		if touched, werr := st.WriteContent(sc, captured); werr == nil {
			sc = touched
		} else {
			return werr
		}
		fmt.Fprintf(out, "created scratch %s (%s) — %s\n", sc.ID, sc.Name, lifespanNote(sc.ExpiresAt, time.Now()))
		return nil
	}

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
