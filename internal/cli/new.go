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
)

// newFlags holds the parsed `sp new` options.
type newFlags struct {
	ttl    time.Duration
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

	// --ttl takes a Go duration (e.g. 168h, 30m). Human "7d"-style parsing
	// lands with the TTL engine in M5; until then the default is applied when
	// the flag is omitted.
	cmd.Flags().DurationVar(&f.ttl, "ttl", 0, "scratch lifespan as a Go duration (e.g. 168h); default 7d when omitted")
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

	if strings.TrimSpace(name) == "" {
		name = generatedName(time.Now())
	}

	sc, path, err := st.Create(store.CreateOptions{
		Name: name,
		Ext:  f.ext,
		TTL:  f.ttl,
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

	fmt.Fprintf(out, "created scratch %s (%s)\n", sc.ID, sc.Name)
	return nil
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
