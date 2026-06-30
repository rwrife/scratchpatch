package cli

import (
	"bytes"
	"strings"
	"testing"
)

// genCompletion runs `sp completion <shell>` against the root command and
// returns stdout plus any error, so tests can assert on both the script body
// and the failure path without touching the real environment.
func genCompletion(t *testing.T, args ...string) (string, error) {
	t.Helper()
	root := NewRootCommand()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(append([]string{"completion"}, args...))
	err := root.Execute()
	return out.String(), err
}

func TestCompletionGeneratesForEachShell(t *testing.T) {
	cases := map[string]string{
		// shell -> a substring that should appear in its generated script.
		"bash": "bash completion",
		"zsh":  "#compdef",
		"fish": "complete",
	}
	for shell, marker := range cases {
		out, err := genCompletion(t, shell)
		if err != nil {
			t.Fatalf("completion %s: unexpected error %v", shell, err)
		}
		if !strings.Contains(out, marker) {
			t.Errorf("completion %s missing marker %q; got first line %q",
				shell, marker, firstLine(out))
		}
		// The script should reference the binary name so completion binds to
		// `sp`, not cobra's placeholder.
		if !strings.Contains(out, "sp") {
			t.Errorf("completion %s should mention the `sp` command name", shell)
		}
	}
}

func TestCompletionRejectsUnknownShell(t *testing.T) {
	out, err := genCompletion(t, "powershell")
	if err == nil {
		t.Fatalf("expected an error for an unsupported shell, got none (out=%q)", out)
	}
	if !strings.Contains(err.Error(), "unsupported shell") {
		t.Errorf("error should explain the unsupported shell; got %v", err)
	}
}

func TestCompletionRequiresExactlyOneArg(t *testing.T) {
	// Zero args is an error (cobra ExactArgs(1)).
	if _, err := genCompletion(t); err == nil {
		t.Error("completion with no shell argument should error")
	}
	// Two args is also an error.
	if _, err := genCompletion(t, "bash", "zsh"); err == nil {
		t.Error("completion with two arguments should error")
	}
}

// firstLine returns the first line of s, for tidy failure messages.
func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
