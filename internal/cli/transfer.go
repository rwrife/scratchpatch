package cli

import (
	"fmt"
	"io"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/rwrife/scratchpatch/internal/store"
)

func newExportCommand() *cobra.Command {
	var out string
	var includeMorgue bool

	cmd := &cobra.Command{
		Use:   "export",
		Short: "Snapshot the whole store to a single .tar.gz",
		Long: "Bundle the store — the index metadata plus every scratch's content —\n" +
			"into one portable .tar.gz you can copy to another machine and `sp\n" +
			"import` there.\n\n" +
			"By default only live scratches are exported. Pass --include-morgue to\n" +
			"also archive soft-deleted scratches still in the morgue.\n\n" +
			"Writes to scratchpatch-export-<timestamp>.tar.gz in the current\n" +
			"directory unless --out names a file. Use --out - to stream the\n" +
			"tarball to stdout (for piping straight into ssh, another tool, etc.).\n\n" +
			"Uses only Go's stdlib archive formats — no external tar/gzip needed.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runExport(cmd, out, includeMorgue)
		},
	}

	cmd.Flags().StringVar(&out, "out", "", "output file (default scratchpatch-export-<timestamp>.tar.gz; \"-\" for stdout)")
	cmd.Flags().BoolVar(&includeMorgue, "include-morgue", false, "also export soft-deleted scratches in the morgue")

	return cmd
}

func runExport(cmd *cobra.Command, out string, includeMorgue bool) error {
	st, err := store.Open()
	if err != nil {
		return err
	}

	var w io.Writer
	var dest string
	if out == "-" {
		w = cmd.OutOrStdout()
	} else {
		if out == "" {
			out = fmt.Sprintf("scratchpatch-export-%s.tar.gz", time.Now().UTC().Format("20060102-150405"))
		}
		f, err := os.Create(out)
		if err != nil {
			return fmt.Errorf("export: create %s: %w", out, err)
		}
		defer f.Close()
		w = f
		dest = out
	}

	if err := st.Export(w, store.ExportOptions{IncludeMorgue: includeMorgue}); err != nil {
		return err
	}

	if dest != "" {
		fmt.Fprintf(cmd.ErrOrStderr(), "exported store → %s\n", dest)
	}
	return nil
}

func newImportCommand() *cobra.Command {
	var merge bool
	var replace bool

	cmd := &cobra.Command{
		Use:   "import <FILE>",
		Short: "Restore scratches from an `sp export` tarball",
		Long: "Read a .tar.gz produced by `sp export` and restore its scratches into\n" +
			"this store. Pass \"-\" as FILE to read the tarball from stdin.\n\n" +
			"Two reconciliation modes:\n\n" +
			"  --merge (default) — add incoming scratches. On an id collision the\n" +
			"    existing scratch is kept and the incoming one is reported as\n" +
			"    skipped. Merge never overwrites anything you already have.\n\n" +
			"  --replace — back up the current store to a timestamped tarball next\n" +
			"    to the store root, then replace it with the archive's contents.\n" +
			"    This is destructive, so it must be requested explicitly; the\n" +
			"    backup keeps it recoverable.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runImport(cmd, args[0], merge, replace)
		},
	}

	cmd.Flags().BoolVar(&merge, "merge", false, "add incoming scratches, never overwriting existing ids (default)")
	cmd.Flags().BoolVar(&replace, "replace", false, "back up then replace the entire store (destructive)")

	return cmd
}

func runImport(cmd *cobra.Command, file string, merge, replace bool) error {
	if merge && replace {
		return fmt.Errorf("import: choose one of --merge or --replace, not both")
	}

	mode := store.ImportMerge
	if replace {
		mode = store.ImportReplace
	}

	st, err := store.Open()
	if err != nil {
		return err
	}

	var r io.Reader
	if file == "-" {
		r = cmd.InOrStdin()
	} else {
		f, err := os.Open(file)
		if err != nil {
			return fmt.Errorf("import: open %s: %w", file, err)
		}
		defer f.Close()
		r = f
	}

	res, err := st.Import(r, mode)
	if err != nil {
		return err
	}

	errOut := cmd.ErrOrStderr()
	if res.BackupPath != "" {
		fmt.Fprintf(errOut, "backed up existing store → %s\n", res.BackupPath)
	}
	fmt.Fprintf(errOut, "imported %d scratch(es)\n", len(res.Added))
	if len(res.Skipped) > 0 {
		fmt.Fprintf(errOut, "skipped %d colliding id(s): %v\n", len(res.Skipped), res.Skipped)
	}
	return nil
}
