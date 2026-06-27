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

// columns is the table header, shared by both the colorized and plain paths so
// they can never drift.
var columns = []string{"ID", "NAME", "AGE", "EXPIRES", "TAGS", "SIZE"}

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
