// Package tui is scratchpatch's optional full-screen browser.
//
// It is a thin interactive shell over the existing store/index/render/secret
// logic — never a second source of truth. The Bubble Tea model reads scratches
// through a small Backend interface (satisfied by *store.Store) and drives the
// same MoveToMorgue / Resurrect / Promote / open-in-$EDITOR operations the CLI
// commands use, so nothing here can hard-delete or diverge from `sp ls`.
//
// Everything the model needs from the outside world is behind Backend and the
// Deps struct: the store operations, "now", and the $EDITOR launcher. That
// keeps the model pure enough to unit-test its update/transition logic with a
// fake backend and no real terminal, per the issue's acceptance criteria.
//
// The scripting/`--json` paths are deliberately untouched: the TUI is opt-in
// (`sp tui`) and the cli layer refuses to launch it without an interactive
// terminal, degrading to a clear "use `sp ls`" error.
package tui

import (
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/rwrife/scratchpatch/internal/index"
	"github.com/rwrife/scratchpatch/internal/secret"
)

// Backend is the slice of store behavior the TUI needs. *store.Store satisfies
// it; tests supply a fake so the model's logic can be exercised without a real
// store or filesystem. Every method mirrors an existing store operation — the
// TUI invents no new mutation.
type Backend interface {
	// ListLive returns live (non-morgued) scratches, newest-first.
	ListLive() ([]index.Scratch, error)
	// ListMorgue returns soft-deleted scratches, newest-first.
	ListMorgue() ([]index.Scratch, error)
	// ReadContent returns a scratch's raw content (live or morgue).
	ReadContent(sc index.Scratch) ([]byte, error)
	// PurgeAt reports when a morgued scratch becomes eligible for hard-delete.
	PurgeAt(sc index.Scratch) (time.Time, bool)
	// MoveToMorgue soft-deletes a live scratch.
	MoveToMorgue(sc index.Scratch) (index.Scratch, error)
	// Resurrect restores a morgued scratch to the living.
	Resurrect(sc index.Scratch) (index.Scratch, error)
}

// EditorFunc opens a scratch's content path in the user's editor, suspending
// and restoring the TUI around it. The cli layer supplies the real one (built
// on the same $EDITOR launcher `sp open` uses); tests pass a no-op.
type EditorFunc func(sc index.Scratch) error

// Deps bundles the model's collaborators so New has a single, testable seam.
type Deps struct {
	Backend Backend
	// Now returns the current time; injected for deterministic tests.
	Now func() time.Time
	// OpenEditor opens a scratch in $EDITOR. May be nil (open is then a no-op
	// that reports the disabled state).
	OpenEditor EditorFunc
}

// view is which pane of scratches is showing.
type view int

const (
	viewLive view = iota
	viewMorgue
)

// pending is a two-step confirmation for a file-moving action.
type pending int

const (
	pendingNone pending = iota
	pendingMorgue
	pendingResurrect
)

// row is a scratch plus the derived display bits the model precomputes once per
// refresh: whether it tripped the secret tripwire and (for morgue rows) its
// purge deadline. Keeping this alongside the scratch avoids re-scanning content
// on every keystroke.
type row struct {
	sc       index.Scratch
	secret   bool
	purgeAt  time.Time
	hasPurge bool
}

// Model is the Bubble Tea model backing `sp tui`.
type Model struct {
	deps Deps

	live   []row
	morgue []row

	view      view
	cursor    int
	filter    string
	filtering bool

	pending pending

	preview       string
	previewSecret bool

	width  int
	height int

	status string
	err    error
	quit   bool
}

// New builds a Model with an initial load already applied. It surfaces a load
// error into the model rather than failing construction so the cli layer can
// still start the program and show the error in-frame.
func New(deps Deps) Model {
	if deps.Now == nil {
		deps.Now = time.Now
	}
	m := Model{deps: deps, view: viewLive}
	m.reload()
	m.syncPreview()
	return m
}

// Init satisfies tea.Model; the TUI has no startup command.
func (m Model) Init() tea.Cmd { return nil }

// reload refreshes both panes from the backend, re-scanning content for the
// secret marker and recomputing purge deadlines. It clamps the cursor so it
// never dangles past a now-shorter list.
func (m *Model) reload() {
	live, lerr := m.deps.Backend.ListLive()
	dead, derr := m.deps.Backend.ListMorgue()
	if lerr != nil {
		m.err = lerr
		return
	}
	if derr != nil {
		m.err = derr
		return
	}
	m.err = nil
	m.live = m.toRows(live, false)
	m.morgue = m.toRows(dead, true)
	m.clampCursor()
}

// toRows converts scratches into display rows, marking secrets and (for morgue
// rows) computing purge deadlines. Content that can't be read just goes
// unmarked — a browse should never fail because one file vanished, matching
// `sp ls`'s best-effort tripwire.
func (m *Model) toRows(scs []index.Scratch, morgue bool) []row {
	rows := make([]row, 0, len(scs))
	for _, sc := range scs {
		r := row{sc: sc}
		if content, err := m.deps.Backend.ReadContent(sc); err == nil {
			r.secret = secret.Tripped(content)
		}
		if morgue {
			if at, ok := m.deps.Backend.PurgeAt(sc); ok {
				r.purgeAt, r.hasPurge = at, true
			}
		}
		rows = append(rows, r)
	}
	return rows
}

// rows returns the currently-visible pane's rows after applying the live
// filter (case-insensitive substring over name, id, and tags).
func (m Model) rows() []row {
	src := m.live
	if m.view == viewMorgue {
		src = m.morgue
	}
	if strings.TrimSpace(m.filter) == "" {
		return src
	}
	needle := strings.ToLower(strings.TrimSpace(m.filter))
	out := make([]row, 0, len(src))
	for _, r := range src {
		if rowMatches(r.sc, needle) {
			out = append(out, r)
		}
	}
	return out
}

// rowMatches reports whether a scratch matches the filter needle by name, id,
// or any tag.
func rowMatches(sc index.Scratch, needle string) bool {
	if strings.Contains(strings.ToLower(sc.Name), needle) {
		return true
	}
	if strings.Contains(strings.ToLower(sc.ID), needle) {
		return true
	}
	for _, t := range sc.Tags {
		if strings.Contains(strings.ToLower(t), needle) {
			return true
		}
	}
	return false
}

// current returns the scratch under the cursor, or false when the visible list
// is empty.
func (m Model) current() (row, bool) {
	rows := m.rows()
	if len(rows) == 0 || m.cursor < 0 || m.cursor >= len(rows) {
		return row{}, false
	}
	return rows[m.cursor], true
}

// clampCursor keeps the cursor within the visible list bounds.
func (m *Model) clampCursor() {
	n := len(m.rows())
	if n == 0 {
		m.cursor = 0
		return
	}
	if m.cursor >= n {
		m.cursor = n - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
}

// syncPreview loads the content preview for the current selection, respecting
// secret scratches: their content is never dumped, only a redaction notice is
// shown, honoring the issue's "does not dump raw credentials by default" rule.
func (m *Model) syncPreview() {
	r, ok := m.current()
	if !ok {
		m.preview = ""
		m.previewSecret = false
		return
	}
	m.previewSecret = r.secret
	if r.secret {
		m.preview = "🔑 this scratch tripped the secret tripwire.\n\n" +
			"Its content is hidden here so credentials aren't dumped to your\n" +
			"terminal. Run `sp scan " + r.sc.ID + "` for masked findings, or open it\n" +
			"in your editor if you really mean to look."
		return
	}
	content, err := m.deps.Backend.ReadContent(r.sc)
	if err != nil {
		m.preview = "(content unavailable — the file may be missing; run `sp doctor`)"
		return
	}
	if len(content) == 0 {
		m.preview = "(empty scratch)"
		return
	}
	m.preview = string(content)
}
