package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// runNewWithStdin executes the root command with a custom stdin (an *os.File
// pipe, so the --stdin TTY guard sees a non-terminal reader) against an
// isolated store. It returns combined stdout+stderr. EDITOR is set to a binary
// that fails loudly if invoked, so tests prove the editor is NOT launched on
// the headless path.
func runNewWithStdin(t *testing.T, stdin string, args ...string) (string, string, error) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("SCRATCHPATCH_HOME", home)
	// A bogus editor: if the headless path ever shells out, the run visibly
	// changes (a "created at" fallback line), which the assertions catch.
	t.Setenv("EDITOR", "this-editor-should-never-run")

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	go func() {
		_, _ = w.WriteString(stdin)
		_ = w.Close()
	}()

	root := NewRootCommand()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetIn(r)
	root.SetArgs(args)
	execErr := root.Execute()
	_ = r.Close()
	return out.String(), home, execErr
}

func TestNewStdinCapturesContentWithoutEditor(t *testing.T) {
	out, home, err := runNewWithStdin(t, "remember to revoke that token\n", "new", "note", "--stdin", "--ext", "txt", "--tag", "ci")
	if err != nil {
		t.Fatalf("new --stdin: %v (out=%s)", err, out)
	}
	// Stable anchor scripts/tests rely on.
	if !strings.Contains(out, "created scratch") {
		t.Errorf("missing stable creation anchor; got %q", out)
	}
	// The editor must NOT have run: no fallback "created at" line, no editor err.
	if strings.Contains(out, "created at") || strings.Contains(out, "this-editor-should-never-run") {
		t.Errorf("editor should not be invoked on --stdin path; got %q", out)
	}

	matches, _ := filepath.Glob(filepath.Join(home, "scratches", "*.txt"))
	if len(matches) != 1 {
		t.Fatalf("expected one .txt scratch, found %v", matches)
	}
	body, _ := os.ReadFile(matches[0])
	if string(body) != "remember to revoke that token\n" {
		t.Errorf("stdin content not persisted; got %q", string(body))
	}
}

func TestNewContentFlag(t *testing.T) {
	out, home, err := runNewWithStdin(t, "", "new", "one-liner", "--content", "quick note", "--ext", "md")
	if err != nil {
		t.Fatalf("new --content: %v (out=%s)", err, out)
	}
	matches, _ := filepath.Glob(filepath.Join(home, "scratches", "*.md"))
	if len(matches) != 1 {
		t.Fatalf("expected one .md scratch, found %v", matches)
	}
	body, _ := os.ReadFile(matches[0])
	if string(body) != "quick note" {
		t.Errorf("content flag not persisted; got %q", string(body))
	}
}

func TestNewContentEmptyIsDeliberate(t *testing.T) {
	// --content "" must create an empty scratch, not fall through to $EDITOR.
	out, home, err := runNewWithStdin(t, "", "new", "empty", "--content", "")
	if err != nil {
		t.Fatalf("new --content '': %v (out=%s)", err, out)
	}
	if strings.Contains(out, "created at") {
		t.Errorf("empty --content should not invoke editor; got %q", out)
	}
	matches, _ := filepath.Glob(filepath.Join(home, "scratches", "*"))
	if len(matches) != 1 {
		t.Fatalf("expected one scratch, found %v", matches)
	}
	if info, _ := os.Stat(matches[0]); info.Size() != 0 {
		t.Errorf("expected empty scratch, size=%d", info.Size())
	}
}

func TestNewFromFile(t *testing.T) {
	src := filepath.Join(t.TempDir(), "seed.json")
	if err := os.WriteFile(src, []byte(`{"ok":true}`), 0o600); err != nil {
		t.Fatalf("seed file: %v", err)
	}
	out, home, err := runNewWithStdin(t, "", "new", "resp", "--from-file", src, "--ext", "json")
	if err != nil {
		t.Fatalf("new --from-file: %v (out=%s)", err, out)
	}
	matches, _ := filepath.Glob(filepath.Join(home, "scratches", "*.json"))
	if len(matches) != 1 {
		t.Fatalf("expected one .json scratch, found %v", matches)
	}
	body, _ := os.ReadFile(matches[0])
	if string(body) != `{"ok":true}` {
		t.Errorf("from-file content not persisted; got %q", string(body))
	}
}

func TestNewConflictingSourcesRejected(t *testing.T) {
	out, _, err := runNewWithStdin(t, "x", "new", "bad", "--stdin", "--content", "y")
	if err == nil {
		t.Fatalf("expected error for conflicting sources, got none (out=%s)", out)
	}
	if !strings.Contains(err.Error(), "only one of") {
		t.Errorf("expected conflicting-source error, got %v", err)
	}
}

func TestNewStdinSecretIsScannedByLs(t *testing.T) {
	// A piped-in secret must be flagged (🔑) by `sp ls`, same as editor scratches.
	secretBody := "AWS_SECRET_ACCESS_KEY=AKIAIOSFODNN7EXAMPLEKEYDATA1234567890ab\n"
	out, home, err := runNewWithStdin(t, secretBody, "new", "leaky", "--stdin")
	if err != nil {
		t.Fatalf("new --stdin: %v (out=%s)", err, out)
	}
	_ = home

	root := NewRootCommand()
	var lsOut bytes.Buffer
	root.SetOut(&lsOut)
	root.SetErr(&lsOut)
	root.SetArgs([]string{"ls"})
	if err := root.Execute(); err != nil {
		t.Fatalf("ls: %v", err)
	}
	if !strings.Contains(lsOut.String(), "🔑") {
		t.Errorf("piped-in secret should be flagged by ls; got %q", lsOut.String())
	}
}
