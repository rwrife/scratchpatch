package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/rwrife/scratchpatch/internal/store"
)

// newPinCommand registers `sp pin <id>`: mark a scratch exempt from the reaper.
// Pinning is the honest alternative to setting an absurd TTL — the scratch keeps
// its real lifespan on paper but `sp reap` refuses to sweep it into the morgue
// while the pin is set.
func newPinCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "pin <id>",
		Short: "Exempt a scratch from reaping",
		Long: "Pin a scratch so `sp reap` never sweeps it into the morgue, no matter\n" +
			"how far past its TTL it drifts. Use this when a scratch matters more\n" +
			"than its expiry implies but doesn't deserve the working tree yet\n" +
			"(`sp promote`). The pin is metadata only — it doesn't move the file or\n" +
			"touch its TTL. Clear it with `sp unpin`. The id may be an unambiguous\n" +
			"prefix.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPin(cmd, args[0], true)
		},
	}
}

// newUnpinCommand registers `sp unpin <id>`: clear a pin so normal TTL rules
// resume and the scratch is once again fair game for the reaper.
func newUnpinCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "unpin <id>",
		Short: "Clear a scratch's pin so reaping resumes",
		Long: "Remove the pin from a scratch, so `sp reap` treats it by its TTL again.\n" +
			"If the scratch is already expired, the next reap will sweep it to the\n" +
			"morgue. The id may be an unambiguous prefix.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPin(cmd, args[0], false)
		},
	}
}

// runPin resolves ref and sets its pin flag to pinned, printing a
// tombstone-flavored confirmation. It short-circuits with a friendly line when
// the pin is already in the requested state so `sp pin` is idempotent and never
// reads like an error.
func runPin(cmd *cobra.Command, ref string, pinned bool) error {
	st, err := store.Open()
	if err != nil {
		return err
	}
	sc, err := resolve(st, ref)
	if err != nil {
		return err
	}

	out := cmd.OutOrStdout()
	if sc.Pinned == pinned {
		if pinned {
			fmt.Fprintf(out, "scratch %s (%s) is already pinned — the reaper still walks on by\n",
				sc.ID, displayName(sc))
		} else {
			fmt.Fprintf(out, "scratch %s (%s) isn't pinned — nothing to release\n",
				sc.ID, displayName(sc))
		}
		return nil
	}

	updated, err := st.SetPin(sc.ID, pinned)
	if err != nil {
		return err
	}

	if pinned {
		fmt.Fprintf(out, "pinned scratch %s (%s) 📌 — exempt from `sp reap` until you `sp unpin` it\n",
			updated.ID, displayName(updated))
	} else {
		fmt.Fprintf(out, "unpinned scratch %s (%s) — its TTL rules again; the reaper may come\n",
			updated.ID, displayName(updated))
	}
	return nil
}
