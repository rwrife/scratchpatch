package cli

import (
	"errors"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/rwrife/scratchpatch/internal/index"
	"github.com/rwrife/scratchpatch/internal/store"
	"github.com/rwrife/scratchpatch/internal/tui"
)

// newTUICommand builds `sp tui`: an optional full-screen Bubble Tea browser
// over the same store the rest of the CLI uses. It is strictly additive —
// scripting and `--json` paths are untouched — and refuses to launch when
// stdout isn't a terminal, pointing scripts at `sp ls` instead.
func newTUICommand() *cobra.Command {
	return &cobra.Command{
		Use:   "tui",
		Short: "Browse and manage scratches in a full-screen terminal UI",
		Long: "Launch an interactive, full-screen browser over your scratch store.\n\n" +
			"Arrow-key (or j/k) through live scratches, press tab to flip to the\n" +
			"morgue, and preview the selected scratch in the side pane. Act on the\n" +
			"selection without leaving the terminal: open in $EDITOR (o), soft-delete\n" +
			"to the morgue (d), or resurrect from it (r) — each file-moving action\n" +
			"asks for a y/N confirmation, and nothing is ever hard-deleted here.\n" +
			"Press / to filter by name, id, or tag; q or esc quits.\n\n" +
			"`sp tui` needs an interactive terminal. For scripting, use `sp ls` or\n" +
			"`sp ls --json`.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTUI(cmd)
		},
	}
}

func runTUI(cmd *cobra.Command) error {
	// The TUI takes over the whole screen and reads raw keys, so it is only
	// meaningful on a real terminal on both ends. Degrade with a clear pointer
	// rather than scribbling escape codes into a pipe or file.
	if !isTerminal(cmd.OutOrStdout()) || !stdinIsTerminal(cmd) {
		return errors.New("sp tui needs an interactive terminal; " +
			"use `sp ls` or `sp ls --json` for scripting")
	}

	st, err := store.Open()
	if err != nil {
		return err
	}

	model := tui.New(tui.Deps{
		Backend:    st,
		OpenEditor: editorLauncher(cmd, st),
	})

	// Full-screen (alt-screen) so the browser restores the user's terminal
	// contents cleanly on quit.
	prog := tea.NewProgram(model, tea.WithAltScreen(),
		tea.WithInput(os.Stdin), tea.WithOutput(os.Stdout))
	if _, err := prog.Run(); err != nil {
		return fmt.Errorf("tui: %w", err)
	}
	return nil
}

// editorLauncher builds the EditorFunc the TUI uses to open a scratch, reusing
// the exact same $EDITOR launcher `sp open` relies on so behavior is identical.
// Bubble Tea's program suspends the alt-screen around a blocking editor via
// tea.ExecProcess, but the model calls this synchronously; to keep the model
// backend-agnostic we run the editor here on the shared launcher, which works
// because Update runs on the program goroutine and Bubble Tea restores the
// terminal on the next render. Returning nil-safe: a missing $EDITOR surfaces
// as an error the model shows in its status line.
func editorLauncher(cmd *cobra.Command, st *store.Store) tui.EditorFunc {
	return func(sc index.Scratch) error {
		path := st.LivePath(sc)
		if err := openInEditor(cmd, path); err != nil {
			return err
		}
		if sc.Live() {
			if _, terr := st.Touch(sc.ID); terr != nil {
				return terr
			}
		}
		return nil
	}
}
