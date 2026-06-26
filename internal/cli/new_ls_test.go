package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// runCmd executes the root command with the given args against an isolated
// store rooted at a temp dir, returning combined stdout+stderr. EDITOR is
// cleared so `new` never tries to spawn an interactive editor in tests.
func runCmd(t *testing.T, args ...string) (string, error) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("SCRATCHPATCH_HOME", home)
	t.Setenv("EDITOR", "")

	root := NewRootCommand()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(args)
	err := root.Execute()
	return out.String(), err
}

func TestNewCreatesScratchWithoutEditor(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SCRATCHPATCH_HOME", home)
	t.Setenv("EDITOR", "")

	root := NewRootCommand()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"new", "mynote", "--no-edit", "--tag", "x", "--tag", "y", "--ext", "txt"})
	if err := root.Execute(); err != nil {
		t.Fatalf("new: %v (out=%s)", err, out.String())
	}

	if !strings.Contains(out.String(), "created scratch") {
		t.Errorf("expected creation confirmation, got %q", out.String())
	}

	// A content file should exist under scratches/ with the txt ext.
	matches, _ := filepath.Glob(filepath.Join(home, "scratches", "*.txt"))
	if len(matches) != 1 {
		t.Fatalf("expected exactly one .txt scratch file, found %v", matches)
	}

	// And it should be listable.
	root2 := NewRootCommand()
	var lsOut bytes.Buffer
	root2.SetOut(&lsOut)
	root2.SetErr(&lsOut)
	root2.SetArgs([]string{"ls"})
	if err := root2.Execute(); err != nil {
		t.Fatalf("ls: %v", err)
	}
	got := lsOut.String()
	if !strings.Contains(got, "mynote") {
		t.Errorf("ls missing scratch name; got %q", got)
	}
	if !strings.Contains(got, "x,y") {
		t.Errorf("ls missing tags; got %q", got)
	}
}

func TestNewAutoGeneratesNameWhenOmitted(t *testing.T) {
	out, err := runCmd(t, "new", "--no-edit")
	if err != nil {
		t.Fatalf("new: %v (out=%s)", err, out)
	}
	if !strings.Contains(out, "scratch-") {
		t.Errorf("expected an auto-generated dated slug name, got %q", out)
	}
}

func TestNewEditorUnsetIsGraceful(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SCRATCHPATCH_HOME", home)
	t.Setenv("EDITOR", "") // explicitly unset

	root := NewRootCommand()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	// No --no-edit, so it tries to open $EDITOR and must fall back cleanly.
	root.SetArgs([]string{"new", "graceful"})
	if err := root.Execute(); err != nil {
		t.Fatalf("new should not error when EDITOR unset, got %v (out=%s)", err, out.String())
	}
	got := out.String()
	if !strings.Contains(got, "EDITOR is not set") {
		t.Errorf("expected graceful EDITOR-unset message, got %q", got)
	}
	// The scratch must still have been created.
	matches, _ := filepath.Glob(filepath.Join(home, "scratches", "*.md"))
	if len(matches) != 1 {
		t.Errorf("scratch should be created even without an editor; found %v", matches)
	}
}

func TestLsEmptyStore(t *testing.T) {
	out, err := runCmd(t, "ls")
	if err != nil {
		t.Fatalf("ls: %v", err)
	}
	if !strings.Contains(out, "no scratches yet") {
		t.Errorf("empty ls should hint how to create; got %q", out)
	}
}

func TestLsPlainOutputToBuffer(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SCRATCHPATCH_HOME", home)
	t.Setenv("EDITOR", "")

	// Seed one scratch.
	create := NewRootCommand()
	var cb bytes.Buffer
	create.SetOut(&cb)
	create.SetErr(&cb)
	create.SetArgs([]string{"new", "alpha", "--no-edit"})
	if err := create.Execute(); err != nil {
		t.Fatalf("seed new: %v", err)
	}

	root := NewRootCommand()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"ls"})
	if err := root.Execute(); err != nil {
		t.Fatalf("ls: %v", err)
	}
	got := out.String()

	// Writing to a bytes.Buffer (not a TTY) must yield colorless, tabbed text.
	if strings.Contains(got, "\x1b[") {
		t.Errorf("ls to a non-TTY must be colorless; got %q", got)
	}
	if !strings.Contains(got, "\t") {
		t.Errorf("ls plain output should be tab-separated; got %q", got)
	}
	if !strings.Contains(got, "ID\tNAME") {
		t.Errorf("ls header missing; got %q", got)
	}
}

func TestGeneratedNameFormat(t *testing.T) {
	got := generatedName(time.Date(2026, 6, 26, 20, 41, 0, 0, time.UTC))
	if got != "scratch-2026-06-26-2041" {
		t.Errorf("generatedName = %q, want scratch-2026-06-26-2041", got)
	}
}

func TestSlugify(t *testing.T) {
	cases := map[string]string{
		"Hello World":   "hello-world",
		"  Foo__Bar!! ": "foo-bar",
		"already-good":  "already-good",
	}
	for in, want := range cases {
		if got := slugify(in); got != want {
			t.Errorf("slugify(%q) = %q, want %q", in, got, want)
		}
	}
}

// isTerminal must treat a bytes.Buffer (and any non-*os.File) as non-TTY.
func TestIsTerminalNonFile(t *testing.T) {
	if isTerminal(&bytes.Buffer{}) {
		t.Error("bytes.Buffer should not be reported as a terminal")
	}
	// An os.Pipe write end is an *os.File but not a char device.
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	defer r.Close()
	defer w.Close()
	if isTerminal(w) {
		t.Error("pipe write end should not be reported as a terminal")
	}
}
