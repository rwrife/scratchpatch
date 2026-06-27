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

	cmd := &cobra.Command{
		Use:   "ls",
		Short: "List live scratches as a colorized table",
		Long: "List the scratches currently in the store: id, name, age, time-to-\n" +
			"expiry, tags, and size. On a terminal the rows are color-coded by how\n" +
			"close each scratch is to expiry (green = fresh, amber = expiring soon,\n" +
			"red = expired). When piped or redirected, output is plain tab-separated\n" +
			"text with no color codes.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runLs(cmd, noColor)
		},
	}

	cmd.Flags().BoolVar(&noColor, "no-color", false, "force plain output even on a TTY")

	return cmd
}

func runLs(cmd *cobra.Command, noColor bool) error {
	st, err := store.Open()
	if err != nil {
		return err
	}

	scratches, err := st.Index().List()
	if err != nil {
		return err
	}

	out := cmd.OutOrStdout()
	color := !noColor && isTerminal(out)

	return render.Table(out, scratches, time.Now(), color)
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
