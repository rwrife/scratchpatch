// Package render is the only package that knows about color.
//
// Everything else in scratchpatch returns plain data; render turns a slice of
// index.Scratch records into a human-facing table. On a TTY it draws a
// colorized lipgloss table whose rows are tinted by how close each scratch is
// to expiry (fresh / expiring-soon / expired). When stdout is not a TTY (a
// pipe, a file, CI) it falls back to a plain, tab-separated table with no
// escape codes, so `sp ls | awk` stays sane.
//
// Keeping color quarantined here means the store, index, and command layers
// never import lipgloss and never reason about terminals.
package render

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/rwrife/scratchpatch/internal/index"
)

// columns is the live-table header, shared by both the colorized and plain
// paths so they can never drift.
var columns = []string{"ID", "NAME", "AGE", "EXPIRES", "TAGS", "SIZE"}

// morgueColumns is the header for `sp ls --morgue`. It swaps the EXPIRES column
// (meaningless once a scratch is dead) for PURGES — the time until reap is
// allowed to hard-delete the content for good.
var morgueColumns = []string{"ID", "NAME", "DELETED", "PURGES", "TAGS", "SIZE"}

// expiringSoon is the window before ExpiresAt within which a scratch is
// considered "expiring soon" and tinted as a warning.
const expiringSoon = 24 * time.Hour

// lifecycle classifies where a scratch sits on the fresh→expired spectrum.
type lifecycle int

const (
	fresh lifecycle = iota
	soon
	expired
)

// classify buckets a scratch by its expiry relative to now.
func classify(s index.Scratch, now time.Time) lifecycle {
	if !now.Before(s.ExpiresAt) {
		return expired
	}
	if s.ExpiresAt.Sub(now) <= expiringSoon {
		return soon
	}
	return fresh
}

// Palette holds the row styles keyed by lifecycle. It's exported mostly so the
// non-color decision stays explicit and testable; callers use Table.
type Palette struct {
	header  lipgloss.Style
	fresh   lipgloss.Style
	soon    lipgloss.Style
	expired lipgloss.Style
}

// defaultPalette is the built-in color scheme: green = fresh, yellow = soon,
// red = expired. Colors are ANSI-256 codes that degrade gracefully on simpler
// terminals.
func defaultPalette() Palette {
	return Palette{
		header:  lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("63")),
		fresh:   lipgloss.NewStyle().Foreground(lipgloss.Color("42")),  // green
		soon:    lipgloss.NewStyle().Foreground(lipgloss.Color("214")), // amber
		expired: lipgloss.NewStyle().Foreground(lipgloss.Color("203")), // red
	}
}

func (p Palette) styleFor(l lifecycle) lipgloss.Style {
	switch l {
	case expired:
		return p.expired
	case soon:
		return p.soon
	default:
		return p.fresh
	}
}

// Table renders scratches to w. When color is true it draws a colorized
// lipgloss table; otherwise it writes a plain tab-separated table. now is
// passed in (rather than read from the clock) so output is deterministic and
// unit-testable.
func Table(w io.Writer, scratches []index.Scratch, now time.Time, color bool) error {
	if len(scratches) == 0 {
		_, err := fmt.Fprintln(w, "no scratches yet — create one with `sp new`")
		return err
	}

	// Stable, newest-first order regardless of what the caller passed.
	rows := make([]index.Scratch, len(scratches))
	copy(rows, scratches)
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].CreatedAt.Equal(rows[j].CreatedAt) {
			return rows[i].ID < rows[j].ID
		}
		return rows[i].CreatedAt.After(rows[j].CreatedAt)
	})

	if color {
		return colorTable(w, rows, now)
	}
	return plainTable(w, rows, now)
}

// rowCells builds the six display strings for a single scratch.
func rowCells(s index.Scratch, now time.Time) []string {
	return []string{
		s.ID,
		nameOrDash(s.Name),
		humanAge(now.Sub(s.CreatedAt)),
		humanExpiry(s.ExpiresAt.Sub(now)),
		tagsOrDash(s.Tags),
		humanSize(s.Size),
	}
}

// plainTable writes a no-escape, tab-separated table suitable for pipes.
func plainTable(w io.Writer, rows []index.Scratch, now time.Time) error {
	var b strings.Builder
	b.WriteString(strings.Join(columns, "\t"))
	b.WriteByte('\n')
	for _, s := range rows {
		b.WriteString(strings.Join(rowCells(s, now), "\t"))
		b.WriteByte('\n')
	}
	_, err := io.WriteString(w, b.String())
	return err
}

// colorTable draws the lipgloss table, tinting each row by lifecycle.
func colorTable(w io.Writer, rows []index.Scratch, now time.Time) error {
	pal := defaultPalette()

	// Compute column widths from the (untinted) cell content so styling
	// doesn't throw off alignment.
	widths := make([]int, len(columns))
	for i, h := range columns {
		widths[i] = lipgloss.Width(h)
	}
	cells := make([][]string, len(rows))
	lifes := make([]lifecycle, len(rows))
	for r, s := range rows {
		cells[r] = rowCells(s, now)
		lifes[r] = classify(s, now)
		for c, val := range cells[r] {
			if wdt := lipgloss.Width(val); wdt > widths[c] {
				widths[c] = wdt
			}
		}
	}

	var b strings.Builder

	// Header.
	headerCells := make([]string, len(columns))
	for i, h := range columns {
		headerCells[i] = pad(h, widths[i])
	}
	b.WriteString(pal.header.Render(strings.Join(headerCells, "  ")))
	b.WriteByte('\n')

	// Body, one tinted line per scratch.
	for r := range rows {
		styled := make([]string, len(columns))
		for c, val := range cells[r] {
			styled[c] = pad(val, widths[c])
		}
		line := strings.Join(styled, "  ")
		b.WriteString(pal.styleFor(lifes[r]).Render(line))
		b.WriteByte('\n')
	}

	_, err := io.WriteString(w, b.String())
	return err
}

// pad right-pads s with spaces to width w (using display width, so wide runes
// don't break alignment).
func pad(s string, w int) string {
	gap := w - lipgloss.Width(s)
	if gap <= 0 {
		return s
	}
	return s + strings.Repeat(" ", gap)
}

func nameOrDash(name string) string {
	if name == "" {
		return "-"
	}
	return name
}

func tagsOrDash(tags []string) string {
	if len(tags) == 0 {
		return "-"
	}
	return strings.Join(tags, ",")
}

// humanAge renders a positive elapsed duration compactly ("3d", "5h", "12m",
// "8s"). It only ever shows the largest meaningful unit — this is a glanceable
// table, not a stopwatch.
func humanAge(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	switch {
	case d >= 24*time.Hour:
		return fmt.Sprintf("%dd", int(d/(24*time.Hour)))
	case d >= time.Hour:
		return fmt.Sprintf("%dh", int(d/time.Hour))
	case d >= time.Minute:
		return fmt.Sprintf("%dm", int(d/time.Minute))
	default:
		return fmt.Sprintf("%ds", int(d/time.Second))
	}
}

// humanExpiry renders time-until-expiry, or "expired" once the deadline has
// passed.
func humanExpiry(remaining time.Duration) string {
	if remaining <= 0 {
		return "expired"
	}
	return "in " + humanAge(remaining)
}

// humanSize renders a byte count in a compact, human-friendly unit.
func humanSize(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%dB", n)
	}
	div, exp := int64(unit), 0
	for v := n / unit; v >= unit; v /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f%cB", float64(n)/float64(div), "KMGTPE"[exp])
}

// MorgueRow pairs a soft-deleted scratch with the moment it becomes eligible
// for hard-deletion. render takes this plain data (computed by the store/config
// layer) rather than reaching for the grace window itself, keeping the "render
// knows nothing but how to draw" boundary intact.
type MorgueRow struct {
	Scratch index.Scratch
	PurgeAt time.Time
}

// MorgueTable renders soft-deleted scratches to w: id, name, when they were
// deleted, time-until-purge, tags, and size. As with Table, color is drawn on a
// TTY and a plain tab-separated table is written otherwise. Rows are tinted by
// purge proximity — amber while there's grace left, red once they're eligible
// for reaping — mirroring the live table's fresh→expired cue.
func MorgueTable(w io.Writer, rows []MorgueRow, now time.Time, color bool) error {
	if len(rows) == 0 {
		_, err := fmt.Fprintln(w, "the morgue is empty — soft-deleted scratches will appear here")
		return err
	}

	// Stable, newest-deleted-first ordering. Fall back to CreatedAt then id so
	// the order is deterministic even if two share a delete time.
	ordered := make([]MorgueRow, len(rows))
	copy(ordered, rows)
	sort.SliceStable(ordered, func(i, j int) bool {
		di, dj := deletedAt(ordered[i].Scratch), deletedAt(ordered[j].Scratch)
		if !di.Equal(dj) {
			return di.After(dj)
		}
		if !ordered[i].Scratch.CreatedAt.Equal(ordered[j].Scratch.CreatedAt) {
			return ordered[i].Scratch.CreatedAt.After(ordered[j].Scratch.CreatedAt)
		}
		return ordered[i].Scratch.ID < ordered[j].Scratch.ID
	})

	if color {
		return colorMorgueTable(w, ordered, now)
	}
	return plainMorgueTable(w, ordered, now)
}

// morgueCells builds the six display strings for a single morgue row.
func morgueCells(r MorgueRow, now time.Time) []string {
	return []string{
		r.Scratch.ID,
		nameOrDash(r.Scratch.Name),
		humanAge(now.Sub(deletedAt(r.Scratch))),
		humanPurge(r.PurgeAt.Sub(now)),
		tagsOrDash(r.Scratch.Tags),
		humanSize(r.Scratch.Size),
	}
}

func plainMorgueTable(w io.Writer, rows []MorgueRow, now time.Time) error {
	var b strings.Builder
	b.WriteString(strings.Join(morgueColumns, "\t"))
	b.WriteByte('\n')
	for _, r := range rows {
		b.WriteString(strings.Join(morgueCells(r, now), "\t"))
		b.WriteByte('\n')
	}
	_, err := io.WriteString(w, b.String())
	return err
}

func colorMorgueTable(w io.Writer, rows []MorgueRow, now time.Time) error {
	pal := defaultPalette()

	widths := make([]int, len(morgueColumns))
	for i, h := range morgueColumns {
		widths[i] = lipgloss.Width(h)
	}
	cells := make([][]string, len(rows))
	doomed := make([]bool, len(rows))
	for r, row := range rows {
		cells[r] = morgueCells(row, now)
		doomed[r] = !now.Before(row.PurgeAt) // past grace → eligible for reap
		for c, val := range cells[r] {
			if wdt := lipgloss.Width(val); wdt > widths[c] {
				widths[c] = wdt
			}
		}
	}

	var b strings.Builder
	headerCells := make([]string, len(morgueColumns))
	for i, h := range morgueColumns {
		headerCells[i] = pad(h, widths[i])
	}
	b.WriteString(pal.header.Render(strings.Join(headerCells, "  ")))
	b.WriteByte('\n')

	for r := range rows {
		styled := make([]string, len(morgueColumns))
		for c, val := range cells[r] {
			styled[c] = pad(val, widths[c])
		}
		line := strings.Join(styled, "  ")
		// Amber while there's grace left, red once past it.
		style := pal.soon
		if doomed[r] {
			style = pal.expired
		}
		b.WriteString(style.Render(line))
		b.WriteByte('\n')
	}

	_, err := io.WriteString(w, b.String())
	return err
}

// deletedAt safely dereferences a scratch's DeletedAt, returning the zero time
// for a (shouldn't-happen-here) live scratch so rendering never panics.
func deletedAt(s index.Scratch) time.Time {
	if s.DeletedAt == nil {
		return time.Time{}
	}
	return *s.DeletedAt
}

// humanPurge renders time-until-hard-deletion, or "now" once a morgue item is
// already past its grace window and eligible for reaping.
func humanPurge(remaining time.Duration) string {
	if remaining <= 0 {
		return "now"
	}
	return "in " + humanAge(remaining)
}

// ReapResult is the plain summary of a reap that ReapSummary renders. The store
// computes it (which scratches were swept to the morgue, which were purged for
// good) and render decides how to phrase and tint it — keeping the
// render-knows-only-color boundary intact for M5's output too.
type ReapResult struct {
	// Swept are the expired live scratches moved into the morgue.
	Swept []index.Scratch
	// Purged are the past-grace morgue items hard-deleted for good.
	Purged []index.Scratch
	// DryRun flips the wording from past-tense ("swept") to conditional
	// ("would sweep") so a preview can never be mistaken for the real thing.
	DryRun bool
}

// ReapSummary writes a human summary of a reap to w. It leads with a one-line
// headline, then lists each affected scratch under a "to the morgue" and a
// "purged for good" section. On a TTY the headline and the purge section are
// tinted (amber for sweeps, red for permanent deletions) to echo the table's
// fresh→expired cue; otherwise it's plain text. A reap that did nothing prints a
// single tidy line.
func ReapSummary(w io.Writer, res ReapResult, color bool) error {
	pal := defaultPalette()

	verbSweep, verbPurge := "swept", "purged"
	if res.DryRun {
		verbSweep, verbPurge = "would sweep", "would purge"
	}

	if len(res.Swept) == 0 && len(res.Purged) == 0 {
		msg := "nothing to reap — every scratch is either fresh or still within its grace window"
		if res.DryRun {
			msg = "dry run: " + msg
		}
		_, err := fmt.Fprintln(w, msg)
		return err
	}

	var b strings.Builder

	headline := fmt.Sprintf("reap: %s %s to the morgue, %s %s for good",
		verbSweep, countScratches(len(res.Swept)),
		verbPurge, countScratches(len(res.Purged)))
	if res.DryRun {
		headline = "dry run — " + headline + " (nothing changed)"
	}
	if color {
		b.WriteString(pal.header.Render(headline))
	} else {
		b.WriteString(headline)
	}
	b.WriteByte('\n')

	if len(res.Swept) > 0 {
		fmt.Fprintf(&b, "\n%s expired → morgue:\n", arrow(res.DryRun))
		writeReapLines(&b, res.Swept, color, pal.soon)
	}
	if len(res.Purged) > 0 {
		fmt.Fprintf(&b, "\n%s past grace → gone:\n", arrow(res.DryRun))
		writeReapLines(&b, res.Purged, color, pal.expired)
	}

	_, err := io.WriteString(w, b.String())
	return err
}

// writeReapLines appends one indented "id  name" line per scratch, tinted with
// style when color is set.
func writeReapLines(b *strings.Builder, scs []index.Scratch, color bool, style lipgloss.Style) {
	for _, sc := range scs {
		line := "  " + sc.ID + "  " + nameOrDash(sc.Name)
		if color {
			line = style.Render(line)
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
}

// arrow picks the bullet for a reap section: a tentative "?" feel for dry runs,
// a decisive arrow for the real thing.
func arrow(dryRun bool) string {
	if dryRun {
		return "·"
	}
	return "→"
}

// countScratches renders "N scratch(es)" with correct pluralization.
func countScratches(n int) string {
	if n == 1 {
		return "1 scratch"
	}
	return fmt.Sprintf("%d scratches", n)
}
