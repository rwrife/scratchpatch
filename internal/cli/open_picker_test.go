package cli

import (
	"bytes"
	"strings"
	"testing"
)

// runResult captures the combined output and error of a root invocation driven
// with a canned stdin, for the picker tests that need to feed the interactive
// prompt. It mirrors session.run but wires SetIn so `sp open`'s no-id picker has
// something to read.
type runResult struct {
	out *bytes.Buffer
	err error
}

// newRootWithInput runs one `sp ...` invocation with stdin bound to input. It
// assumes SCRATCHPATCH_HOME/EDITOR are already set by the enclosing session, so
// it shares that store. Because a *bytes.Reader is not a TTY, the picker takes
// its deterministic non-TTY numbered path — exactly what these tests want.
func newRootWithInput(t *testing.T, input string, args ...string) runResult {
	t.Helper()
	root := NewRootCommand()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetIn(strings.NewReader(input))
	root.SetArgs(args)
	err := root.Execute()
	return runResult{out: &out, err: err}
}

// TestOpenPickerNoScratchesIsFriendly verifies `sp open` with no id and an
// empty store prints a gentle pointer instead of an error.
func TestOpenPickerNoScratchesIsFriendly(t *testing.T) {
	s := newSession(t)
	out, err := s.run("open")
	if err != nil {
		t.Fatalf("open on empty store should not error; got %v (out=%s)", err, out)
	}
	if !strings.Contains(out, "no live scratches") {
		t.Errorf("expected a friendly empty-store message; got %q", out)
	}
}

// TestOpenPickerNumberedSelectsAndOpens drives the no-id picker down its
// non-TTY numbered path (buffers aren't a TTY). Choosing "1" should resolve to
// the sole scratch; with $EDITOR unset the command falls back to printing the
// scratch's path, which proves the picked scratch flowed into the open logic.
func TestOpenPickerNumberedSelectsAndOpens(t *testing.T) {
	s := newSession(t)
	id := s.newScratchID("pick-me")

	root := newRootWithInput(t, "1\n", "open")
	out := root.out.String()
	if err := root.err; err != nil {
		t.Fatalf("open picker: %v (out=%s)", err, out)
	}
	// The numbered list should have offered our scratch...
	if !strings.Contains(out, id) {
		t.Errorf("picker list should include scratch id %q; got %q", id, out)
	}
	// ...and selecting it should have driven the open path. $EDITOR is unset in
	// tests, so we see the "is at <path>" fallback naming the scratch.
	if !strings.Contains(out, "is at") || !strings.Contains(out, id) {
		t.Errorf("selecting the scratch should open it (path fallback expected); got %q", out)
	}
}

// TestOpenPickerCancelIsNoOp verifies that backing out of the picker (an empty
// line on the numbered path cancels) changes nothing and reports gently.
func TestOpenPickerCancelIsNoOp(t *testing.T) {
	s := newSession(t)
	_ = s.newScratchID("leave-me")

	root := newRootWithInput(t, "\n", "open")
	out := root.out.String()
	if err := root.err; err != nil {
		t.Fatalf("cancelling the picker should not error; got %v (out=%s)", err, out)
	}
	if !strings.Contains(out, "nothing opened") {
		t.Errorf("a cancelled picker should say nothing was opened; got %q", out)
	}
}

// TestOpenStillTakesExplicitID guards the original behavior: `sp open <id>`
// bypasses the picker entirely and opens the named scratch.
func TestOpenStillTakesExplicitID(t *testing.T) {
	s := newSession(t)
	id := s.newScratchID("direct")

	out, err := s.run("open", id)
	if err != nil {
		t.Fatalf("open <id>: %v (out=%s)", err, out)
	}
	// EDITOR unset → path fallback naming the scratch.
	if !strings.Contains(out, id) {
		t.Errorf("open <id> should act on that scratch; got %q", out)
	}
}
