package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/rwrife/scratchpatch/internal/index"
	"github.com/rwrife/scratchpatch/internal/ttl"
)

// Styles for the browser. Colors echo the `sp ls` palette (green/amber/red by
// expiry, violet header) so the TUI and the table feel like one tool.
var (
	titleStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("63"))
	selStyle     = lipgloss.NewStyle().Bold(true).Reverse(true)
	freshStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	soonStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	expiredStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("203"))
	dimStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("242"))
	footerStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	warnStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("214"))
	previewBox   = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1)
)

// View renders the full-screen browser: a title bar, a two-column body (list +
// preview), and a help/status footer. It degrades gracefully before the first
// WindowSizeMsg by assuming a sane default size.
func (m Model) View() string {
	if m.quit {
		return ""
	}

	width, height := m.width, m.height
	if width <= 0 {
		width = 80
	}
	if height <= 0 {
		height = 24
	}

	var b strings.Builder
	b.WriteString(m.titleBar())
	b.WriteByte('\n')

	// Body height budget: total minus title, filter line, footer.
	bodyHeight := height - 4
	if bodyHeight < 3 {
		bodyHeight = 3
	}

	listW := width / 2
	if listW < 24 {
		listW = 24
	}
	previewW := width - listW - 4
	if previewW < 10 {
		previewW = 10
	}

	list := m.listPane(listW, bodyHeight)
	preview := m.previewPane(previewW, bodyHeight)
	body := lipgloss.JoinHorizontal(lipgloss.Top, list, "  ", preview)
	b.WriteString(body)
	b.WriteByte('\n')

	b.WriteString(m.filterLine())
	b.WriteByte('\n')
	b.WriteString(m.footer())
	return b.String()
}

// titleBar shows the tool name, active pane, and counts.
func (m Model) titleBar() string {
	pane := "live"
	if m.view == viewMorgue {
		pane = "morgue"
	}
	counts := fmt.Sprintf("%d live · %d morgue", len(m.live), len(m.morgue))
	return titleStyle.Render("scratchpatch") + "  " +
		dimStyle.Render("["+pane+"]") + "  " + dimStyle.Render(counts)
}

// listPane renders the scrollable scratch list for the active pane, tinted by
// expiry (live) or purge proximity (morgue), with the cursor row reversed.
func (m Model) listPane(width, height int) string {
	rows := m.rows()
	now := m.deps.Now()

	if m.err != nil {
		return expiredStyle.Render("error: " + m.err.Error())
	}
	if len(rows) == 0 {
		if strings.TrimSpace(m.filter) != "" {
			return dimStyle.Render("(no scratches match \"" + m.filter + "\")")
		}
		if m.view == viewMorgue {
			return dimStyle.Render("the morgue is empty")
		}
		return dimStyle.Render("no scratches yet — create one with `sp new`")
	}

	// Simple scroll window so the cursor stays visible in tall lists.
	start := 0
	if m.cursor >= height {
		start = m.cursor - height + 1
	}
	end := start + height
	if end > len(rows) {
		end = len(rows)
	}

	var b strings.Builder
	for i := start; i < end; i++ {
		line := m.renderRow(rows[i], now, width)
		if i == m.cursor {
			line = selStyle.Render(padRight(stripToWidth(line, width), width))
		}
		b.WriteString(line)
		if i < end-1 {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

// renderRow formats a single list row: id, name (with 🔑 for secrets), age, and
// expiry-or-purge, tinted by lifecycle.
func (m Model) renderRow(r row, now time.Time, width int) string {
	name := r.sc.Name
	if name == "" {
		name = "-"
	}
	if r.secret {
		name = "🔑 " + name
	}

	var right string
	var style lipgloss.Style
	if m.view == viewMorgue {
		right = purgeLabel(r, now)
		style = soonStyle
		if r.hasPurge && !now.Before(r.purgeAt) {
			style = expiredStyle
		}
	} else {
		right = expiryLabel(r.sc, now)
		style = styleForExpiry(r.sc, now)
	}

	left := fmt.Sprintf("%s  %-16s", r.sc.ID, truncate(name, 16))
	age := humanDur(now.Sub(r.sc.CreatedAt))
	line := fmt.Sprintf("%s %4s  %s", left, age, right)
	return style.Render(stripToWidth(line, width))
}

// previewPane shows the selected scratch's content (or the redaction notice for
// secrets), wrapped in a rounded box sized to the available height.
func (m Model) previewPane(width, height int) string {
	content := m.preview
	if content == "" {
		content = dimStyle.Render("(nothing selected)")
	}
	// Bound the preview to the visible height so a huge scratch doesn't blow
	// past the frame.
	lines := strings.Split(content, "\n")
	max := height - 2 // account for the box border
	if max < 1 {
		max = 1
	}
	if len(lines) > max {
		lines = lines[:max]
		lines[max-1] = truncate(lines[max-1], width-2) + " …"
	}
	for i, ln := range lines {
		lines[i] = stripToWidth(ln, width-2)
	}
	body := strings.Join(lines, "\n")
	if m.previewSecret {
		body = warnStyle.Render(body)
	}
	return previewBox.Width(width).Height(height - 2).Render(body)
}

// filterLine shows the active filter box while filtering, or the pending
// confirmation prompt, or the last status message.
func (m Model) filterLine() string {
	if m.pending != pendingNone {
		var what string
		switch m.pending {
		case pendingMorgue:
			what = "move this scratch to the morgue?"
		case pendingResurrect:
			what = "resurrect this scratch?"
		}
		return warnStyle.Render("confirm: " + what + "  [y/N]")
	}
	if m.filtering {
		return titleStyle.Render("/") + m.filter + dimStyle.Render("▏")
	}
	if m.filter != "" {
		return dimStyle.Render("filter: " + m.filter + "  (/ to edit, esc clears)")
	}
	if m.status != "" {
		return dimStyle.Render(m.status)
	}
	return ""
}

// footer is the keybinding cheat sheet, context-sensitive to the active pane.
func (m Model) footer() string {
	keys := []string{
		"↑/↓ move",
		"tab live/morgue",
		"/ filter",
		"o open",
	}
	if m.view == viewLive {
		keys = append(keys, "d rm→morgue")
	} else {
		keys = append(keys, "r resurrect")
	}
	keys = append(keys, "R refresh", "q quit")
	return footerStyle.Render(strings.Join(keys, "  ·  "))
}

// --- small formatting helpers (kept local so the TUI owns its own layout) ---

func styleForExpiry(sc index.Scratch, now time.Time) lipgloss.Style {
	switch ttl.Classify(sc.ExpiresAt, now) {
	case ttl.Expired:
		return expiredStyle
	case ttl.Soon:
		return soonStyle
	default:
		return freshStyle
	}
}

func expiryLabel(sc index.Scratch, now time.Time) string {
	remaining := sc.ExpiresAt.Sub(now)
	if remaining <= 0 {
		return "expired"
	}
	return "in " + humanDur(remaining)
}

func purgeLabel(r row, now time.Time) string {
	if !r.hasPurge {
		return "-"
	}
	remaining := r.purgeAt.Sub(now)
	if remaining <= 0 {
		return "purges now"
	}
	return "purges in " + humanDur(remaining)
}

// humanDur renders a positive duration as its largest unit, matching render's
// glanceable style ("3d", "5h", "12m", "8s").
func humanDur(d time.Duration) string {
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

// truncate shortens s to at most width display columns, adding an ellipsis when
// it cuts. Uses lipgloss.Width for wide-rune correctness.
func truncate(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= width {
		return s
	}
	// Trim runes until it fits, leaving room for the ellipsis.
	runes := []rune(s)
	for len(runes) > 0 && lipgloss.Width(string(runes))+1 > width {
		runes = runes[:len(runes)-1]
	}
	return string(runes) + "…"
}

// stripToWidth clips s to width display columns without adding an ellipsis,
// used to keep styled rows from overflowing the pane.
func stripToWidth(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= width {
		return s
	}
	runes := []rune(s)
	for len(runes) > 0 && lipgloss.Width(string(runes)) > width {
		runes = runes[:len(runes)-1]
	}
	return string(runes)
}

// padRight pads s with spaces to width display columns.
func padRight(s string, width int) string {
	gap := width - lipgloss.Width(s)
	if gap <= 0 {
		return s
	}
	return s + strings.Repeat(" ", gap)
}
