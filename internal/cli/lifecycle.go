package cli

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/rwrife/scratchpatch/internal/index"
	"github.com/rwrife/scratchpatch/internal/store"
)

// resolve turns a user-supplied id-or-prefix into a scratch, mapping the
// store's lookup errors to messages a CLI user can act on. It's the single
// entry point the lifecycle commands (cat/open/rm/resurrect) use so prefix
// handling and error wording stay consistent.
func resolve(st *store.Store, ref string) (index.Scratch, error) {
	sc, err := st.Resolve(ref)
	if err == nil {
		return sc, nil
	}
	switch {
	case errors.Is(err, store.ErrAmbiguousID):
		// The store's message already lists the candidates; surface it as-is
		// but make the "type more characters" hint explicit.
		return index.Scratch{}, fmt.Errorf("%w — add more characters to disambiguate", err)
	case errors.Is(err, index.ErrNotFound):
		return index.Scratch{}, fmt.Errorf("no scratch matches %q", ref)
	default:
		return index.Scratch{}, err
	}
}

func newCatCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "cat <id>",
		Short: "Print a scratch's contents",
		Long: "Print the contents of a scratch to stdout. Works on live scratches\n" +
			"and on ones sitting in the morgue. The id may be an unambiguous prefix.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCat(cmd, args[0])
		},
	}
}

func runCat(cmd *cobra.Command, ref string) error {
	st, err := store.Open()
	if err != nil {
		return err
	}
	sc, err := resolve(st, ref)
	if err != nil {
		return err
	}
	content, err := st.ReadContent(sc)
	if err != nil {
		return err
	}
	_, err = cmd.OutOrStdout().Write(content)
	return err
}

func newOpenCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "open <id>",
		Short: "Re-open a scratch in $EDITOR",
		Long: "Re-open an existing scratch in $EDITOR. The id may be an unambiguous\n" +
			"prefix. A morgued scratch is opened in place in the morgue (resurrect\n" +
			"it first if you want it back among the living). When $EDITOR is unset,\n" +
			"the scratch's path is printed so you can open it yourself.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runOpen(cmd, args[0])
		},
	}
}

func runOpen(cmd *cobra.Command, ref string) error {
	st, err := store.Open()
	if err != nil {
		return err
	}
	sc, err := resolve(st, ref)
	if err != nil {
		return err
	}

	path := st.LivePath(sc)
	out := cmd.OutOrStdout()

	if err := openInEditor(cmd, path); err != nil {
		// Mirror `sp new`: a missing/failed editor is not fatal — tell the
		// user where the scratch lives so it's never inaccessible.
		fmt.Fprintln(cmd.ErrOrStderr(), err)
		fmt.Fprintf(out, "scratch %s is at %s\n", sc.ID, path)
		return nil
	}

	// Refresh recorded size in case the edit changed it. Only meaningful for
	// live scratches; Touch reads from scratches/, so skip morgued ones.
	if sc.Live() {
		if _, terr := st.Touch(sc.ID); terr != nil {
			return terr
		}
	}

	fmt.Fprintf(out, "opened scratch %s (%s)\n", sc.ID, displayName(sc))
	return nil
}

func newRmCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "rm <id>",
		Short: "Soft-delete a scratch into the morgue",
		Long: "Move a scratch into the morgue. This is a soft-delete: the content is\n" +
			"not destroyed, just relocated, and can be restored with `sp resurrect`.\n" +
			"Only `sp reap` (M5) ever hard-deletes, and only past the grace window.\n" +
			"The id may be an unambiguous prefix.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRm(cmd, args[0])
		},
	}
}

func runRm(cmd *cobra.Command, ref string) error {
	st, err := store.Open()
	if err != nil {
		return err
	}
	sc, err := resolve(st, ref)
	if err != nil {
		return err
	}
	if sc.Morgued() {
		return fmt.Errorf("scratch %s is already in the morgue", sc.ID)
	}
	if _, err := st.MoveToMorgue(sc); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(),
		"moved scratch %s (%s) to the morgue — restore with `sp resurrect %s`\n",
		sc.ID, displayName(sc), sc.ID)
	return nil
}

func newResurrectCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "resurrect <id>",
		Aliases: []string{"restore"},
		Short:   "Restore a scratch from the morgue",
		Long: "Pull a soft-deleted scratch back out of the morgue and into the live\n" +
			"set. The id may be an unambiguous prefix; it's resolved against the\n" +
			"morgue, so a prefix only needs to be unique among dead scratches.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runResurrect(cmd, args[0])
		},
	}
	return cmd
}

func runResurrect(cmd *cobra.Command, ref string) error {
	st, err := store.Open()
	if err != nil {
		return err
	}
	sc, err := resolve(st, ref)
	if err != nil {
		return err
	}
	if !sc.Morgued() {
		return fmt.Errorf("scratch %s is not in the morgue", sc.ID)
	}
	if _, err := st.Resurrect(sc); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(),
		"resurrected scratch %s (%s) — it's live again\n", sc.ID, displayName(sc))
	return nil
}

// displayName returns the scratch's name, or its id when unnamed, for use in
// human-facing confirmation lines.
func displayName(sc index.Scratch) string {
	if sc.Name == "" {
		return sc.ID
	}
	return sc.Name
}
