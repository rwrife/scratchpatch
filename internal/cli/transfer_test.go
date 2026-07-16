package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestExportImportCLIRoundTrip exercises the full command path: create a
// scratch, `sp export` to a file, then `sp import` into a second store and
// confirm the scratch's content survives the trip.
func TestExportImportCLIRoundTrip(t *testing.T) {
	src := newSession(t)
	src.newScratchID("carryover")

	dir := t.TempDir()
	tarball := filepath.Join(dir, "snap.tar.gz")
	if out, err := src.run("export", "--out", tarball); err != nil {
		t.Fatalf("export: %v (out=%s)", err, out)
	}
	if _, err := os.Stat(tarball); err != nil {
		t.Fatalf("export produced no file: %v", err)
	}

	dst := newSession(t) // fresh SCRATCHPATCH_HOME
	out, err := dst.run("import", tarball)
	if err != nil {
		t.Fatalf("import: %v (out=%s)", err, out)
	}
	if !strings.Contains(out, "imported 1") {
		t.Errorf("import should report one scratch; got %q", out)
	}

	ls, _ := dst.run("ls")
	if !strings.Contains(ls, "carryover") {
		t.Errorf("imported store should list the scratch; got %q", ls)
	}
}

// TestImportMergeRejectsBothModes: --merge and --replace are mutually exclusive.
func TestImportMergeReplaceConflict(t *testing.T) {
	s := newSession(t)
	dir := t.TempDir()
	tarball := filepath.Join(dir, "snap.tar.gz")
	if _, err := s.run("export", "--out", tarball); err != nil {
		t.Fatalf("export: %v", err)
	}
	if _, err := s.run("import", tarball, "--merge", "--replace"); err == nil {
		t.Error("import with both --merge and --replace should error")
	}
}
