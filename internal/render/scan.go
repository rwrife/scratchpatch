// scan.go renders the secret tripwire's findings for `sp scan`.
//
// As with every other command, the detector (internal/secret) returns plain
// data and render decides how to phrase and tint it. render never sees a full
// secret: the Masked field arrives already redacted from the detector, and this
// file only ever prints that masked preview. A clean scratch gets a reassuring
// (green) line in scratchpatch's tombstone voice; a tripped one gets an amber
// header and one red line per finding, naming the line number, the rule, and
// the masked value.
//
// The --json path here mirrors ls/doctor: color-free, personality-free, stable
// keys, findings always an array (never null) so a script can iterate
// unconditionally and gate on `.tripped`.
package render

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// ScanFinding is render's flat view of one secret-detector hit: the kind of
// secret, the 1-based line it sat on, the human rule label, and a masked
// preview safe to print. render imports neither the store nor the secret
// package; the CLI adapts those into this shape (same pattern as DoctorOrphan).
type ScanFinding struct {
	Kind   string
	Line   int
	Rule   string
	Masked string
}

// ScanReportData is the plain summary `sp scan` renders: which scratch was
// scanned and every place it tripped. An empty Findings slice means the scratch
// is clean.
type ScanReportData struct {
	ID       string
	Name     string
	Findings []ScanFinding
}

// tripped reports whether the scanned scratch hit anything.
func (d ScanReportData) tripped() bool { return len(d.Findings) > 0 }

// ScanReport writes a secret-scan report to w. A clean scratch gets a single
// green line; a tripped one leads with an amber headline naming the count, then
// lists each finding on its own red line as "L<line>  <rule>  <masked>", and
// closes with a pointer at the safe next step (promote blocks unless
// --allow-secrets). On a non-TTY it's plain text. The masked values are printed
// verbatim from the detector — this function never has, and never prints, a raw
// secret.
func ScanReport(w io.Writer, d ScanReportData, color bool) error {
	pal := defaultPalette()
	var b strings.Builder

	if !d.tripped() {
		writeLine(&b, fmt.Sprintf("clean bill of health — no secrets found in %s (%s)",
			d.ID, nameOrDash(d.Name)), color, pal.fresh)
		_, err := io.WriteString(w, b.String())
		return err
	}

	writeLine(&b, fmt.Sprintf("tripwire! %s in %s (%s) — do NOT let this out alive",
		countFindings(len(d.Findings)), d.ID, nameOrDash(d.Name)), color, pal.header)

	for _, f := range d.Findings {
		line := fmt.Sprintf("  L%-4d  %-32s  %s", f.Line, f.Rule, f.Masked)
		writeLine(&b, line, color, pal.expired)
	}

	fmt.Fprintf(&b, "\nvalues are masked; `sp promote` will refuse this scratch "+
		"unless you pass --allow-secrets.\n")

	_, err := io.WriteString(w, b.String())
	return err
}

// ScanJSON is the scriptable record for `sp scan --json`: the scanned scratch's
// id/name, a one-field `tripped` summary so a script can gate without inspecting
// the array, and the findings themselves. Like ls/doctor JSON it is color- and
// personality-free, and findings is always a (possibly empty) array so the
// shape never flips to null on a clean scratch.
type ScanJSON struct {
	ID       string            `json:"id"`
	Name     string            `json:"name"`
	Tripped  bool              `json:"tripped"`
	Findings []ScanFindingJSON `json:"findings"`
}

// ScanFindingJSON is the scriptable view of a single finding. Masked carries the
// redacted preview only; there is deliberately no field for the raw value.
type ScanFindingJSON struct {
	Kind   string `json:"kind"`
	Line   int    `json:"line"`
	Rule   string `json:"rule"`
	Masked string `json:"masked"`
}

// ScanReportJSON writes a ScanReportData to w as a single ScanJSON object. The
// findings slice is always emitted as an array (never null), and no raw secret
// is ever included — only the detector's masked preview.
func ScanReportJSON(w io.Writer, d ScanReportData) error {
	findings := make([]ScanFindingJSON, 0, len(d.Findings))
	for _, f := range d.Findings {
		findings = append(findings, ScanFindingJSON{
			Kind:   f.Kind,
			Line:   f.Line,
			Rule:   f.Rule,
			Masked: f.Masked,
		})
	}
	rec := ScanJSON{
		ID:       d.ID,
		Name:     d.Name,
		Tripped:  d.tripped(),
		Findings: findings,
	}
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	return enc.Encode(rec)
}

// countFindings renders "N secret(s)" with correct pluralization, matching the
// countScratches/countOrphans family so reports read naturally for 1 or N.
func countFindings(n int) string {
	if n == 1 {
		return "1 secret"
	}
	return fmt.Sprintf("%d secrets", n)
}
