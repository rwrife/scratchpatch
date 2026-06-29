package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestDoctorReportsHealthyStore: with one real scratch and nothing amiss, the
// command says the store is healthy and exits cleanly.
func TestDoctorReportsHealthyStore(t *testing.T) {
	s := newSession(t)
	s.newScratchID("tidy")

	out, err := s.run("doctor")
	if err != nil {
		t.Fatalf("doctor: %v (out=%s)", err, out)
	}
	if !strings.Contains(out, "healthy") {
		t.Errorf("doctor on a clean store should report healthy; got %q", out)
	}
	if !strings.Contains(out, "1 scratch live") {
		t.Errorf("doctor should count the live scratch; got %q", out)
	}
}

// TestDoctorFindsOrphanAndMissing: an orphaned file and a missing-content entry
// both surface through the full command wiring, and the run stays read-only.
func TestDoctorFindsOrphanAndMissing(t *testing.T) {
	s := newSession(t)
	id := s.newScratchID("ghost")

	// Remove the indexed file → missing content.
	indexed := filepath.Join(s.home, "scratches", id+".txt")
	if err := os.Remove(indexed); err != nil {
		t.Fatalf("remove indexed content: %v", err)
	}
	// Drop a file nothing indexes → orphan.
	orphan := filepath.Join(s.home, "scratches", "deadbeef.md")
	if err := os.WriteFile(orphan, []byte("loose\n"), 0o600); err != nil {
		t.Fatalf("write orphan: %v", err)
	}

	out, err := s.run("doctor", "--no-color")
	if err != nil {
		t.Fatalf("doctor: %v (out=%s)", err, out)
	}

	for _, want := range []string{
		"frowns",
		"orphaned content",
		"deadbeef.md",
		"missing content",
		id,
		"ghost",
		"nothing was changed",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("doctor output missing %q; got:\n%s", want, out)
		}
	}

	// Read-only: the orphan must still be there afterward (doctor didn't tidy).
	if _, err := os.Stat(orphan); err != nil {
		t.Errorf("doctor must not delete the orphan; stat err = %v", err)
	}
}

// TestDoctorPlainOutputHasNoColor: piped/non-TTY output (a bytes.Buffer here) is
// plain even when problems exist, so the report stays scriptable.
func TestDoctorPlainOutputHasNoColor(t *testing.T) {
	s := newSession(t)
	// Create a real scratch first so the store layout (scratches/) exists, then
	// drop an orphan beside it to give doctor a problem to report.
	s.newScratchID("real")
	orphan := filepath.Join(s.home, "scratches", "cafef00d.md")
	if err := os.WriteFile(orphan, []byte("x\n"), 0o600); err != nil {
		t.Fatalf("write orphan: %v", err)
	}

	out, err := s.run("doctor")
	if err != nil {
		t.Fatalf("doctor: %v (out=%s)", err, out)
	}
	if strings.Contains(out, "\x1b[") {
		t.Errorf("doctor output to a buffer must be colorless; got %q", out)
	}
}
