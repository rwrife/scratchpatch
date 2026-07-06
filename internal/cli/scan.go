package cli

import (
	"github.com/spf13/cobra"

	"github.com/rwrife/scratchpatch/internal/index"
	"github.com/rwrife/scratchpatch/internal/render"
	"github.com/rwrife/scratchpatch/internal/secret"
	"github.com/rwrife/scratchpatch/internal/store"
)

// ErrSecretsFound is returned by `sp scan` when a scratch trips the secret
// tripwire. It carries no message: the scan report has already told the user
// everything, so main maps this sentinel to a non-zero exit code WITHOUT
// printing a redundant error line. This is what lets `sp scan <id>` act as a
// gate in pre-commit hooks and CI (`sp scan x || block`) while staying quiet on
// stderr.
var ErrSecretsFound = errSecretsFoundError{}

type errSecretsFoundError struct{}

func (errSecretsFoundError) Error() string { return "" }

// errSecretsFound is the unexported handle the command returns; it is the same
// value as the exported ErrSecretsFound so callers can errors.Is against either.
var errSecretsFound error = ErrSecretsFound

func newScanCommand() *cobra.Command {
	var noColor bool
	var asJSON bool

	cmd := &cobra.Command{
		Use:   "scan <id>",
		Short: "Check a scratch for leaked secrets (masked findings)",
		Long: "Run the secret tripwire over a single scratch and report anything that\n" +
			"looks like a credential: AWS access keys, PEM private-key headers,\n" +
			"`*_API_KEY=`/`TOKEN=` style assignments, and long high-entropy tokens.\n\n" +
			"Findings are reported by line number with the offending value MASKED —\n" +
			"scan never echoes a full secret back to your terminal. It is read-only\n" +
			"and changes nothing; it just tells you whether a scratch is safe to\n" +
			"`sp promote` into a repo (which blocks on a tripped scratch unless you\n" +
			"pass --allow-secrets).\n\n" +
			"The id may be an unambiguous prefix, and works on morgued scratches too.\n" +
			"Pass --json for a stable, machine-readable object (no color, no flavor)\n" +
			"suitable for scripting: `sp scan <id> --json | jq '.tripped'`.\n\n" +
			"Exit status is non-zero when secrets are found, so scan slots into\n" +
			"pre-commit hooks and CI: `sp scan <id> || echo blocked`.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runScan(cmd, args[0], noColor, asJSON)
		},
	}

	cmd.Flags().BoolVar(&noColor, "no-color", false, "force plain output even on a TTY")
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit a JSON object instead of a report (for scripting)")

	return cmd
}

func runScan(cmd *cobra.Command, ref string, noColor, asJSON bool) error {
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

	findings := secret.Scan(content)
	data := toScanReport(sc, findings)

	out := cmd.OutOrStdout()
	if asJSON {
		if err := render.ScanReportJSON(out, data); err != nil {
			return err
		}
	} else {
		color := !noColor && isTerminal(out)
		if err := render.ScanReport(out, data, color); err != nil {
			return err
		}
	}

	// Non-zero exit when the scratch tripped, so scan is usable as a gate in
	// hooks and CI. cobra prints RunE errors, so use a silent sentinel and let
	// the caller (main) map it to an exit code without a noisy message — the
	// report already said everything.
	if len(findings) > 0 {
		return errSecretsFound
	}
	return nil
}

// toScanReport flattens the store scratch + detector findings into the render
// layer's plain view, matching the adapter pattern doctor/reap use so render
// never imports the store or secret packages.
func toScanReport(sc index.Scratch, findings []secret.Finding) render.ScanReportData {
	rows := make([]render.ScanFinding, len(findings))
	for i, f := range findings {
		rows[i] = render.ScanFinding{
			Kind:   string(f.Kind),
			Line:   f.Line,
			Rule:   f.Rule,
			Masked: f.Masked,
		}
	}
	return render.ScanReportData{
		ID:       sc.ID,
		Name:     sc.Name,
		Findings: rows,
	}
}
