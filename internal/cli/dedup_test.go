package cli

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

// TestDedupCleanStoreCLI: a store of unique scratches reports no duplicates.
func TestDedupCleanStoreCLI(t *testing.T) {
	s := newSession(t)
	if out, err := s.run("new", "a", "--content", "alpha\n", "--ext", "txt"); err != nil {
		t.Fatalf("new a: %v (%s)", err, out)
	}
	if out, err := s.run("new", "b", "--content", "beta\n", "--ext", "txt"); err != nil {
		t.Fatalf("new b: %v (%s)", err, out)
	}

	out, err := s.run("dedup", "--no-color")
	if err != nil {
		t.Fatalf("dedup: %v (%s)", err, out)
	}
	if !strings.Contains(out, "no duplicates") {
		t.Errorf("clean store should report no duplicates; got %q", out)
	}
}

// TestDedupReportsAndCollapsesCLI: identical content is reported as a cluster;
// default is read-only; --collapse morgues the redundant copy (never hard-
// deletes), keeping the canonical live.
func TestDedupReportsAndCollapsesCLI(t *testing.T) {
	s := newSession(t)
	body := "duplicate body\n"
	for _, name := range []string{"first", "second", "third"} {
		if out, err := s.run("new", name, "--content", body, "--ext", "txt"); err != nil {
			t.Fatalf("new %s: %v (%s)", name, err, out)
		}
	}

	// Read-only report first.
	out, err := s.run("dedup", "--no-color")
	if err != nil {
		t.Fatalf("dedup: %v (%s)", err, out)
	}
	if !strings.Contains(out, "cluster") || !strings.Contains(out, "canonical") {
		t.Errorf("dedup should report a cluster with a canonical; got %q", out)
	}
	if !strings.Contains(out, "--collapse") {
		t.Errorf("read-only dedup should point at --collapse; got %q", out)
	}
	// Read-only: still 3 live scratch files on disk.
	if got := len(globTxt(t, s.home)); got != 3 {
		t.Fatalf("read-only dedup changed the store: %d live files, want 3", got)
	}

	// Now collapse.
	out, err = s.run("dedup", "--collapse", "--no-color")
	if err != nil {
		t.Fatalf("dedup --collapse: %v (%s)", err, out)
	}
	if !strings.Contains(out, "collapsed") || !strings.Contains(out, "morgue") {
		t.Errorf("collapse should confirm the move; got %q", out)
	}

	// Canonical stays live; the two redundant copies are morgued (not gone).
	live := globTxt(t, filepath.Join(s.home, "scratches"))
	if len(live) != 1 {
		t.Errorf("after collapse: %d live files, want 1", len(live))
	}
	morgue := globTxt(t, filepath.Join(s.home, "morgue"))
	if len(morgue) != 2 {
		t.Errorf("after collapse: %d morgue files, want 2 (nothing hard-deleted)", len(morgue))
	}
}

// TestDedupJSONCLI: --json is a stable, colorless object with the clean flag
// and cluster data.
func TestDedupJSONCLI(t *testing.T) {
	s := newSession(t)
	body := "same\n"
	for _, name := range []string{"x", "y"} {
		if out, err := s.run("new", name, "--content", body, "--ext", "txt"); err != nil {
			t.Fatalf("new %s: %v (%s)", name, err, out)
		}
	}

	out, err := s.run("dedup", "--json")
	if err != nil {
		t.Fatalf("dedup --json: %v (%s)", err, out)
	}
	if strings.Contains(out, "\x1b[") {
		t.Errorf("dedup --json must be colorless; got %q", out)
	}

	var rec struct {
		Clean        bool `json:"clean"`
		ClusterCount int  `json:"clusterCount"`
		Clusters     []struct {
			Count   int `json:"count"`
			Members []struct {
				Canonical bool `json:"canonical"`
			} `json:"members"`
		} `json:"clusters"`
		Collapsed any `json:"collapsed"`
	}
	if err := json.Unmarshal([]byte(out), &rec); err != nil {
		t.Fatalf("invalid JSON: %v (%q)", err, out)
	}
	if rec.Clean {
		t.Errorf("two identical scratches should report clean=false")
	}
	if rec.ClusterCount != 1 || len(rec.Clusters) != 1 || rec.Clusters[0].Count != 2 {
		t.Fatalf("unexpected cluster shape: %+v", rec)
	}
	if rec.Collapsed != nil {
		t.Errorf("collapsed should be null without --collapse; got %v", rec.Collapsed)
	}
}

// globTxt returns the *.txt files under dir (or under dir/scratches when dir is
// a store home passed bare).
func globTxt(t *testing.T, dir string) []string {
	t.Helper()
	// If dir is a store home (contains scratches/), glob live scratches.
	pattern := filepath.Join(dir, "*.txt")
	matches, _ := filepath.Glob(pattern)
	if len(matches) == 0 {
		matches, _ = filepath.Glob(filepath.Join(dir, "scratches", "*.txt"))
	}
	return matches
}
