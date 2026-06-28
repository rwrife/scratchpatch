package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// session runs a sequence of commands against one shared store, so a test can
// create a scratch and then act on it (rm/cat/resurrect) the way a user would.
// runCmd (in new_ls_test.go) gives each call its own temp home; session keeps
// one home for the whole test.
type session struct {
	t    *testing.T
	home string
}

func newSession(t *testing.T) *session {
	t.Helper()
	home := t.TempDir()
	t.Setenv("SCRATCHPATCH_HOME", home)
	t.Setenv("EDITOR", "") // never spawn a real editor in tests
	return &session{t: t, home: home}
}

// run executes one root command invocation and returns combined stdout+stderr.
func (s *session) run(args ...string) (string, error) {
	s.t.Helper()
	root := NewRootCommand()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(args)
	err := root.Execute()
	return out.String(), err
}

// newScratchID creates a named scratch and digs its id back out of the index by
// listing — the create confirmation prints the name, not the id, so we read the
// id from the on-disk content filename.
func (s *session) newScratchID(name string) string {
	s.t.Helper()
	if out, err := s.run("new", name, "--no-edit", "--ext", "txt"); err != nil {
		s.t.Fatalf("new %q: %v (out=%s)", name, err, out)
	}
	matches, _ := filepath.Glob(filepath.Join(s.home, "scratches", "*.txt"))
	if len(matches) == 0 {
		s.t.Fatalf("no scratch file created for %q", name)
	}
	// Most-recently created is fine for these single-scratch-per-name tests;
	// when several exist, callers pass distinct exts or check explicitly.
	base := filepath.Base(matches[len(matches)-1])
	return strings.TrimSuffix(base, ".txt")
}

func TestRmMovesToMorgueAndLsReflectsIt(t *testing.T) {
	s := newSession(t)
	id := s.newScratchID("trash-me")

	out, err := s.run("rm", id)
	if err != nil {
		t.Fatalf("rm: %v (out=%s)", err, out)
	}
	if !strings.Contains(out, "morgue") {
		t.Errorf("rm should confirm the move to the morgue; got %q", out)
	}

	// Live ls no longer shows it.
	liveOut, _ := s.run("ls")
	if strings.Contains(liveOut, "trash-me") {
		t.Errorf("live ls should not show a soft-deleted scratch; got %q", liveOut)
	}

	// Morgue ls does, with a PURGES column.
	morgueOut, err := s.run("ls", "--morgue")
	if err != nil {
		t.Fatalf("ls --morgue: %v", err)
	}
	if !strings.Contains(morgueOut, "trash-me") {
		t.Errorf("morgue ls should show the soft-deleted scratch; got %q", morgueOut)
	}
	if !strings.Contains(morgueOut, "PURGES") {
		t.Errorf("morgue ls should have a PURGES column; got %q", morgueOut)
	}
}

func TestResurrectBringsScratchBack(t *testing.T) {
	s := newSession(t)
	id := s.newScratchID("comeback")

	if out, err := s.run("rm", id); err != nil {
		t.Fatalf("rm: %v (out=%s)", err, out)
	}
	out, err := s.run("resurrect", id)
	if err != nil {
		t.Fatalf("resurrect: %v (out=%s)", err, out)
	}
	if !strings.Contains(out, "live again") {
		t.Errorf("resurrect should confirm the scratch is live; got %q", out)
	}

	// Back in live ls, gone from the morgue.
	liveOut, _ := s.run("ls")
	if !strings.Contains(liveOut, "comeback") {
		t.Errorf("resurrected scratch should reappear in live ls; got %q", liveOut)
	}
	morgueOut, _ := s.run("ls", "--morgue")
	if strings.Contains(morgueOut, "comeback") {
		t.Errorf("morgue should be empty after resurrect; got %q", morgueOut)
	}
}

func TestCatPrintsContentLiveAndMorgue(t *testing.T) {
	s := newSession(t)
	id := s.newScratchID("readme")

	// Write some content directly into the scratch file.
	matches, _ := filepath.Glob(filepath.Join(s.home, "scratches", id+".txt"))
	if len(matches) != 1 {
		t.Fatalf("expected one scratch file, got %v", matches)
	}
	if err := os.WriteFile(matches[0], []byte("hello from cat\n"), 0o600); err != nil {
		t.Fatalf("write content: %v", err)
	}

	out, err := s.run("cat", id)
	if err != nil {
		t.Fatalf("cat: %v (out=%s)", err, out)
	}
	if !strings.Contains(out, "hello from cat") {
		t.Errorf("cat should print content; got %q", out)
	}

	// cat still works once the scratch is in the morgue.
	if _, err := s.run("rm", id); err != nil {
		t.Fatalf("rm: %v", err)
	}
	out, err = s.run("cat", id)
	if err != nil {
		t.Fatalf("cat (morgue): %v", err)
	}
	if !strings.Contains(out, "hello from cat") {
		t.Errorf("cat should print morgued content; got %q", out)
	}
}

func TestCatPrefixResolution(t *testing.T) {
	s := newSession(t)
	id := s.newScratchID("prefixed")
	matches, _ := filepath.Glob(filepath.Join(s.home, "scratches", id+".txt"))
	if err := os.WriteFile(matches[0], []byte("prefix body\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	// A 4-char prefix of the 8-char id should resolve.
	out, err := s.run("cat", id[:4])
	if err != nil {
		t.Fatalf("cat by prefix: %v (out=%s)", err, out)
	}
	if !strings.Contains(out, "prefix body") {
		t.Errorf("cat by prefix should print content; got %q", out)
	}
}

func TestLifecycleUnknownIDErrors(t *testing.T) {
	s := newSession(t)
	for _, sub := range []string{"cat", "rm", "resurrect", "open"} {
		out, err := s.run(sub, "nope9999")
		if err == nil {
			t.Errorf("%s on unknown id should error; out=%q", sub, out)
		}
		if !strings.Contains(err.Error(), "no scratch matches") {
			t.Errorf("%s error should mention no match; got %v", sub, err)
		}
	}
}

func TestRmAlreadyMorguedErrors(t *testing.T) {
	s := newSession(t)
	id := s.newScratchID("double")
	if _, err := s.run("rm", id); err != nil {
		t.Fatalf("first rm: %v", err)
	}
	_, err := s.run("rm", id)
	if err == nil {
		t.Error("rm of an already-morgued scratch should error")
	}
}

func TestResurrectLiveErrors(t *testing.T) {
	s := newSession(t)
	id := s.newScratchID("stillhere")
	_, err := s.run("resurrect", id)
	if err == nil {
		t.Error("resurrect of a live scratch should error")
	}
}

func TestLsMorgueEmpty(t *testing.T) {
	s := newSession(t)
	out, err := s.run("ls", "--morgue")
	if err != nil {
		t.Fatalf("ls --morgue: %v", err)
	}
	if !strings.Contains(out, "morgue is empty") {
		t.Errorf("empty morgue should say so; got %q", out)
	}
}
