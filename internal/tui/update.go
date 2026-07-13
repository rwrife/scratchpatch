package tui

import (
	tea "github.com/charmbracelet/bubbletea"
)

// Update is the Bubble Tea update loop. It routes window-size and key events;
// all mutation goes through the same store operations the CLI uses, and any
// file-moving action (morgue/resurrect) requires an explicit second keypress
// via the pending-confirmation state, satisfying the "confirmation for anything
// that moves files" criterion.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil
	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

// handleKey dispatches a keypress. Filter-entry mode captures text; otherwise
// keys are commands. A pending confirmation intercepts y/n first so an
// accidental keystroke can't move a file.
func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.pending != pendingNone {
		return m.handlePending(msg)
	}
	if m.filtering {
		return m.handleFilterKey(msg)
	}

	switch msg.String() {
	case "q", "esc", "ctrl+c":
		m.quit = true
		return m, tea.Quit

	case "up", "k":
		m.moveCursor(-1)
	case "down", "j":
		m.moveCursor(1)
	case "g", "home":
		m.cursor = 0
		m.syncPreview()
	case "G", "end":
		m.cursor = len(m.rows()) - 1
		m.clampCursor()
		m.syncPreview()

	case "tab", "m":
		m.toggleView()

	case "/":
		m.filtering = true
		m.status = ""

	case "o", "enter":
		return m.openSelection()

	case "d", "x":
		m.requestMorgue()
	case "r":
		m.requestResurrect()

	case "R":
		m.reload()
		m.syncPreview()
		m.status = "refreshed"
	}
	return m, nil
}

// handlePending resolves a two-step confirmation: y/enter commits the pending
// action, anything else cancels it. This is the single choke point through
// which morgue and resurrect actually touch files.
func (m Model) handlePending(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y", "Y", "enter":
		p := m.pending
		m.pending = pendingNone
		return m.commitPending(p)
	default:
		m.pending = pendingNone
		m.status = "cancelled"
		return m, nil
	}
}

// commitPending performs the confirmed file move through the backend, then
// reloads so both panes reflect the change. Errors surface into the status line
// rather than crashing the browser.
func (m Model) commitPending(p pending) (tea.Model, tea.Cmd) {
	r, ok := m.current()
	if !ok {
		m.status = "nothing selected"
		return m, nil
	}
	switch p {
	case pendingMorgue:
		if _, err := m.deps.Backend.MoveToMorgue(r.sc); err != nil {
			m.status = "rm failed: " + err.Error()
			return m, nil
		}
		m.status = "moved " + r.sc.ID + " to the morgue"
	case pendingResurrect:
		if _, err := m.deps.Backend.Resurrect(r.sc); err != nil {
			m.status = "resurrect failed: " + err.Error()
			return m, nil
		}
		m.status = "resurrected " + r.sc.ID
	}
	m.reload()
	m.syncPreview()
	return m, nil
}

// handleFilterKey edits the filter string live; esc cancels back to the full
// list, enter accepts the filter and returns to navigation.
func (m Model) handleFilterKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.filtering = false
		m.filter = ""
		m.cursor = 0
		m.syncPreview()
	case "enter":
		m.filtering = false
		m.clampCursor()
		m.syncPreview()
	case "backspace":
		if m.filter != "" {
			m.filter = m.filter[:len(m.filter)-1]
		}
		m.cursor = 0
		m.syncPreview()
	default:
		if msg.Type == tea.KeyRunes {
			m.filter += string(msg.Runes)
			m.cursor = 0
			m.syncPreview()
		}
	}
	return m, nil
}

// moveCursor shifts the selection by delta, clamped, and refreshes the preview.
func (m *Model) moveCursor(delta int) {
	n := len(m.rows())
	if n == 0 {
		m.cursor = 0
		return
	}
	m.cursor += delta
	if m.cursor < 0 {
		m.cursor = 0
	}
	if m.cursor >= n {
		m.cursor = n - 1
	}
	m.syncPreview()
}

// toggleView flips between the live and morgue panes, resetting the cursor and
// clearing any half-typed filter so the two views don't share a stale needle.
func (m *Model) toggleView() {
	if m.view == viewLive {
		m.view = viewMorgue
	} else {
		m.view = viewLive
	}
	m.cursor = 0
	m.filter = ""
	m.filtering = false
	m.pending = pendingNone
	m.syncPreview()
}

// openSelection launches $EDITOR on the current scratch, then reloads (an edit
// may have changed size/secret status). A nil editor reports the disabled
// state instead of doing nothing silently.
func (m Model) openSelection() (tea.Model, tea.Cmd) {
	r, ok := m.current()
	if !ok {
		m.status = "nothing to open"
		return m, nil
	}
	if m.deps.OpenEditor == nil {
		m.status = "editor unavailable ($EDITOR not set)"
		return m, nil
	}
	if err := m.deps.OpenEditor(r.sc); err != nil {
		m.status = "open failed: " + err.Error()
		return m, nil
	}
	m.reload()
	m.syncPreview()
	m.status = "opened " + r.sc.ID
	return m, nil
}

// requestMorgue arms a morgue confirmation for a live selection. It refuses on
// the morgue pane (already dead) so `d` there is a clear no-op message.
func (m *Model) requestMorgue() {
	if m.view != viewLive {
		m.status = "already in the morgue — press r to resurrect"
		return
	}
	if _, ok := m.current(); !ok {
		m.status = "nothing selected"
		return
	}
	m.pending = pendingMorgue
}

// requestResurrect arms a resurrect confirmation for a morgue selection. On the
// live pane there's nothing to resurrect, so it says so.
func (m *Model) requestResurrect() {
	if m.view != viewMorgue {
		m.status = "resurrect works on morgue scratches — press tab to switch"
		return
	}
	if _, ok := m.current(); !ok {
		m.status = "nothing selected"
		return
	}
	m.pending = pendingResurrect
}
