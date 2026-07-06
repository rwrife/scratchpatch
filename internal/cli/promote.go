package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/rwrife/scratchpatch/internal/index"
	"github.com/rwrife/scratchpatch/internal/secret"
	"github.com/rwrife/scratchpatch/internal/store"
)

// promoteFlags holds the parsed `sp promote` options.
type promoteFlags struct {
	force        bool
	noOpen       bool
	allowSecrets bool
}

func newPromoteCommand() *cobra.Command {
	var f promoteFlags

	cmd := &cobra.Command{
		Use:   "promote <id> [dest]",
		Short: "Graduate a scratch into the current repo",
		Long: "Move a scratch out of the store and into the working tree \u2014 the escape\n" +
			"hatch for the throwaway that turned out to matter. The content file is\n" +
			"relocated into the current directory (or [dest]) and the scratch is\n" +
			"dropped from the index: once promoted it's the repo's to keep, and the\n" +
			"reaper can't touch it.\n\n" +
			"If [dest] is an existing directory the file is placed inside it under a\n" +
			"slug of its name; otherwise [dest] is the full target path. Promoting\n" +
			"never overwrites an existing file unless --force is given. The id may be\n" +
			"an unambiguous prefix.\n\n" +
			"Before moving, promote runs the secret tripwire over the scratch and\n" +
			"refuses to graduate one that looks like it holds a credential (API keys,\n" +
			"private keys, `TOKEN=`/`SECRET=` assignments). Run `sp scan <id>` to see\n" +
			"the masked findings, or pass --allow-secrets to promote it anyway.",
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			dest := ""
			if len(args) == 2 {
				dest = args[1]
			}
			return runPromote(cmd, args[0], dest, f)
		},
	}

	cmd.Flags().BoolVar(&f.force, "force", false, "overwrite the destination if a file is already there")
	cmd.Flags().BoolVar(&f.noOpen, "no-open", false, "don't open the promoted file in $EDITOR after moving")
	cmd.Flags().BoolVar(&f.allowSecrets, "allow-secrets", false, "promote even if the scratch trips the secret tripwire")

	return cmd
}

func runPromote(cmd *cobra.Command, ref, dest string, f promoteFlags) error {
	st, err := store.Open()
	if err != nil {
		return err
	}
	sc, err := resolve(st, ref)
	if err != nil {
		return err
	}

	// Secret tripwire: refuse to graduate a scratch that looks like it holds a
	// credential into the working tree, unless the user explicitly overrides.
	// This is the last line of defense before a leaked key lands in a repo where
	// it might get committed. Checked before any move so a blocked promote
	// changes nothing.
	if !f.allowSecrets {
		if blocked, serr := promoteSecretGuard(st, sc); serr != nil {
			return serr
		} else if blocked != nil {
			return blocked
		}
	}

	target, err := promoteTarget(sc, dest)
	if err != nil {
		return err
	}

	// Guard against promoting a scratch onto its own store file, which the
	// move would happily "succeed" at while corrupting state.
	if abs, aerr := filepath.Abs(target); aerr == nil {
		if same, serr := sameFile(abs, st.LivePath(sc)); serr == nil && same {
			return fmt.Errorf("destination %s is the scratch's own store file", target)
		}
	}

	if err := st.Promote(sc, target, f.force); err != nil {
		return promoteError(err, target)
	}

	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "promoted scratch %s (%s) \u2192 %s \u2014 it escapes the reaper and joins the working tree\n", sc.ID, displayName(sc), target)

	if !f.noOpen {
		if oerr := openInEditor(cmd, target); oerr != nil {
			// The move already succeeded; a missing/failed editor just means
			// the user opens it themselves. Never treat this as fatal.
			fmt.Fprintln(cmd.ErrOrStderr(), oerr)
		}
	}
	return nil
}

// promoteTarget resolves the destination the scratch's content should land at.
// With no dest, the file goes into the current directory under a friendly slug.
// A dest that is (or looks like) a directory places the file inside it; any
// other dest is treated as the full target path, so `sp promote x keep.md`
// renames on the way out.
func promoteTarget(sc index.Scratch, dest string) (string, error) {
	filename := promoteFilename(sc)

	if dest == "" {
		return filename, nil
	}

	if isDirDest(dest) {
		return filepath.Join(dest, filename), nil
	}
	return dest, nil
}

// promoteFilename builds the default on-disk name for a promoted scratch: a
// slug of its name (falling back to its id) plus its extension, so a scratch
// named "Deploy Notes" lands as deploy-notes.md rather than a bare hex id.
func promoteFilename(sc index.Scratch) string {
	base := slugify(sc.Name)
	if base == "" {
		base = sc.ID
	}
	if sc.Ext != "" {
		base += "." + sc.Ext
	}
	return base
}

// isDirDest reports whether dest should be treated as a directory to drop the
// file into: an existing directory, or a path written with a trailing
// separator or a bare "." / ".." that clearly names a directory.
func isDirDest(dest string) bool {
	if info, err := os.Stat(dest); err == nil {
		return info.IsDir()
	}
	if dest == "." || dest == ".." {
		return true
	}
	if os.IsPathSeparator(dest[len(dest)-1]) {
		return true
	}
	return false
}

// sameFile reports whether two paths refer to the same on-disk file, so we can
// refuse a no-op/destructive promote onto the scratch's own store file.
func sameFile(a, b string) (bool, error) {
	ai, err := os.Stat(a)
	if err != nil {
		return false, err
	}
	bi, err := os.Stat(b)
	if err != nil {
		return false, err
	}
	return os.SameFile(ai, bi), nil
}

// promoteError maps the store's promote failures onto CLI-actionable wording.
func promoteError(err error, target string) error {
	if errors.Is(err, store.ErrDestinationExists) {
		return fmt.Errorf("%s already exists \u2014 pass --force to overwrite it", target)
	}
	return err
}

// promoteSecretGuard runs the secret tripwire over the scratch's content and,
// if it trips, returns a blocking error explaining how to inspect (`sp scan`)
// or override (--allow-secrets). A nil error and nil block mean the scratch is
// clean (or its content couldn't be read as text, in which case we don't block
// on a read error — promote itself will surface any real content problem). The
// error deliberately names the finding count but not the secret values; use
// `sp scan` to see the masked details.
func promoteSecretGuard(st *store.Store, sc index.Scratch) (blocked error, err error) {
	content, rerr := st.ReadContent(sc)
	if rerr != nil {
		// Don't turn a content-read problem into a secret block; let the actual
		// promote path report it. Returning nil,nil means "not blocked here".
		return nil, nil
	}
	findings := secret.Scan(content)
	if len(findings) == 0 {
		return nil, nil
	}
	n := "secret"
	if len(findings) != 1 {
		n = "secrets"
	}
	return fmt.Errorf(
		"refusing to promote %s (%s): %d %s detected \u2014 run `sp scan %s` to see "+
			"the (masked) findings, or pass --allow-secrets to promote anyway",
		sc.ID, displayName(sc), len(findings), n, sc.ID), nil
}
