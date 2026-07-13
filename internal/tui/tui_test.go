package tui

import (
	"errors"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/rwrife/scratchpatch/internal/index"
)

// fakeBackend is an in-memory Backend for exercising the model's transitions
// without a real store or filesystem. It records mutations so tests can assert
// the TUI drives the same operations the CLI would.
type fakeBackend struct {
	live    []index.Scratch
	morgue  []index.Scratch
	content map[string][]byte

	morgued     []string
	resurrected []string

	morgueErr error
}

func (f *fakeBackend) ListLive() ([]index.Scratch, error)   { return f.live, nil }
func (f *fakeBackend) ListMorgue() ([]index.Scratch, error) { return f.morgue, nil }

func (f *fakeBackend) ReadContent(sc index.Scratch) ([]byte, error) {
	if b, ok := f.content[sc.ID]; ok {
		return b, nil
	}
	return nil, errors.New("missing")
}

func (f *fakeBackend) PurgeAt(sc index.Scratch) (time.Time, bool) {
	if sc.DeletedAt == nil {
		return time.Time{}, false
	}
	return sc.DeletedAt.Add(72 * time.Hour), true
}

func (f *fakeBackend) MoveToMorgue(sc index.Scratch) (index.Scratch, error) {
	if f.morgueErr != nil {
		return index.Scratch{}, f.morgueErr
	}
	f.morgued = append(f.morgued, sc.ID)
	// Move from live to morgue for a faithful reload.
	now := time.Now()
	sc.DeletedAt = &now
	f.live = removeByID(f.live, sc.ID)
	f.morgue = append(f.morgue, sc)
	return sc, nil
}

func (f *fakeBackend) Resurrect(sc index.Scratch) (index.Scratch, error) {
	f.resurrected = append(f.resurrected, sc.ID)
	sc.DeletedAt = nil
	f.morgue = removeByID(f.morgue, sc.ID)
	f.live = append(f.live, sc)
	return sc, nil
}

func removeByID(scs []index.Scratch, id string) []index.Scratch {
	out := scs[:0:0]
	for _, s := range scs {
		if s.ID != id {
			out = append(out, s)
		}
	}
	return out
}

func fixedNow() time.Time { return time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC) }

func sampleModel(fb *fakeBackend) Model {
	return New(Deps{Backend: fb, Now: fixedNow, OpenEditor: nil})
}

func key(s string) tea.KeyMsg {
	switch s {
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "backspace":
		return tea.KeyMsg{Type: tea.KeyBackspace}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	}
}

func send(m Model, keys ...string) Model {
	for _, k := range keys {
		nm, _ := m.Update(key(k))
		m = nm.(Model)
	}
	return m
}

func baseBackend() *fakeBackend {
	del := fixedNow().Add(-1 * time.Hour)
	return &fakeBackend{
		live: []index.Scratch{
			{ID: "aaaa1111", Name: "notes", CreatedAt: fixedNow().Add(-2 * time.Hour), ExpiresAt: fixedNow().Add(48 * time.Hour), Tags: []string{"work"}},
			{ID: "bbbb2222", Name: "todo", CreatedAt: fixedNow().Add(-1 * time.Hour), ExpiresAt: fixedNow().Add(10 * time.Hour)},
		},
		morgue: []index.Scratch{
			{ID: "cccc3333", Name: "old", CreatedAt: fixedNow().Add(-5 * time.Hour), ExpiresAt: fixedNow(), DeletedAt: &del},
		},
		content: map[string][]byte{
			"aaaa1111": []byte("hello notes"),
			"bbbb2222": []byte("AWS_SECRET_ACCESS_KEY=AKIAIOSFODNN7EXAMPLE1234567890abcdef"),
			"cccc3333": []byte("dead content"),
		},
	}
}

func TestInitialLoadPopulatesPanes(t *testing.T) {
	m := sampleModel(baseBackend())
	if got := len(m.live); got != 2 {
		t.Fatalf("live rows = %d, want 2", got)
	}
	if got := len(m.morgue); got != 1 {
		t.Fatalf("morgue rows = %d, want 1", got)
	}
	if r, ok := m.current(); !ok || r.sc.ID != "aaaa1111" {
		t.Fatalf("initial selection = %+v, ok=%v", r, ok)
	}
}

func TestSecretPreviewIsRedacted(t *testing.T) {
	m := sampleModel(baseBackend())
	m = send(m, "down") // move to the secret scratch bbbb2222
	if !m.previewSecret {
		t.Fatal("expected secret scratch to be flagged")
	}
	if wantNot := "AKIA"; contains(m.preview, wantNot) {
		t.Fatalf("preview leaked raw secret content: %q", m.preview)
	}
	if !contains(m.preview, "tripwire") {
		t.Fatalf("preview should explain the redaction, got %q", m.preview)
	}
}

func TestToggleViewSwitchesPanes(t *testing.T) {
	m := sampleModel(baseBackend())
	m = send(m, "tab")
	if m.view != viewMorgue {
		t.Fatal("tab should switch to the morgue")
	}
	if r, ok := m.current(); !ok || r.sc.ID != "cccc3333" {
		t.Fatalf("morgue selection = %+v ok=%v", r, ok)
	}
}

func TestMorgueRequiresConfirmation(t *testing.T) {
	fb := baseBackend()
	m := sampleModel(fb)
	// Arm, then cancel with a non-confirm key: no move should happen.
	m = send(m, "d")
	if m.pending != pendingMorgue {
		t.Fatal("d should arm a morgue confirmation")
	}
	m = send(m, "n")
	if len(fb.morgued) != 0 {
		t.Fatalf("cancelled confirmation still moved: %v", fb.morgued)
	}
	// Arm and confirm with y: exactly one move, and it leaves the live pane.
	m = send(m, "d", "y")
	if len(fb.morgued) != 1 || fb.morgued[0] != "aaaa1111" {
		t.Fatalf("confirmed morgue = %v, want [aaaa1111]", fb.morgued)
	}
	if len(m.live) != 1 {
		t.Fatalf("live pane should shrink to 1 after morgue, got %d", len(m.live))
	}
}

func TestResurrectOnlyOnMorguePane(t *testing.T) {
	fb := baseBackend()
	m := sampleModel(fb)
	// r on the live pane is a no-op with a hint.
	m = send(m, "r")
	if m.pending != pendingNone {
		t.Fatal("r on live pane should not arm resurrect")
	}
	// Switch to morgue, resurrect with confirm.
	m = send(m, "tab", "r")
	if m.pending != pendingResurrect {
		t.Fatal("r on morgue pane should arm resurrect")
	}
	m = send(m, "y")
	if len(fb.resurrected) != 1 || fb.resurrected[0] != "cccc3333" {
		t.Fatalf("resurrected = %v, want [cccc3333]", fb.resurrected)
	}
}

func TestMorgueErrorSurfacesInStatus(t *testing.T) {
	fb := baseBackend()
	fb.morgueErr = errors.New("disk full")
	m := sampleModel(fb)
	m = send(m, "d", "y")
	if !contains(m.status, "rm failed") {
		t.Fatalf("expected failure in status, got %q", m.status)
	}
	if len(m.live) != 2 {
		t.Fatal("failed morgue should not change the live pane")
	}
}

func TestFilterNarrowsList(t *testing.T) {
	m := sampleModel(baseBackend())
	m = send(m, "/", "t", "o", "d", "o")
	if got := len(m.rows()); got != 1 {
		t.Fatalf("filter 'todo' matched %d rows, want 1", got)
	}
	// esc clears the filter back to the full list.
	m = send(m, "esc")
	if got := len(m.rows()); got != 2 {
		t.Fatalf("after esc, rows = %d, want 2", got)
	}
}

func TestQuitSetsQuitFlag(t *testing.T) {
	m := sampleModel(baseBackend())
	nm, cmd := m.Update(key("q"))
	m = nm.(Model)
	if !m.quit {
		t.Fatal("q should set the quit flag")
	}
	if cmd == nil {
		t.Fatal("q should return a quit command")
	}
}

func TestOpenWithoutEditorReportsDisabled(t *testing.T) {
	m := sampleModel(baseBackend()) // OpenEditor is nil
	m = send(m, "o")
	if !contains(m.status, "editor unavailable") {
		t.Fatalf("expected disabled-editor status, got %q", m.status)
	}
}

func TestViewRendersWithoutPanic(t *testing.T) {
	m := sampleModel(baseBackend())
	nm, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = nm.(Model)
	out := m.View()
	if !contains(out, "scratchpatch") {
		t.Fatalf("view should include the title, got:\n%s", out)
	}
	if !contains(out, "notes") {
		t.Fatalf("view should list a scratch name, got:\n%s", out)
	}
}

func contains(haystack, needle string) bool {
	return len(needle) == 0 || (len(haystack) >= len(needle) && indexOf(haystack, needle) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
