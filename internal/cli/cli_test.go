package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestVersionCommandOutput(t *testing.T) {
	root := NewRootCommand()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"version"})

	if err := root.Execute(); err != nil {
		t.Fatalf("version command failed: %v", err)
	}

	got := out.String()
	if !strings.Contains(got, "scratchpatch ") {
		t.Errorf("version output missing product name; got %q", got)
	}
	if !strings.Contains(got, Tagline) {
		t.Errorf("version output missing tagline; got %q", got)
	}
}

func TestRootHelpHasSubcommands(t *testing.T) {
	root := NewRootCommand()
	if c, _, err := root.Find([]string{"version"}); err != nil || c.Name() != "version" {
		t.Fatalf("expected to find version subcommand, got %v (err %v)", c, err)
	}
}
