package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestDoctorJSONHealthyStore: `sp doctor --json` on a clean store emits a single
// valid, colorless JSON object with healthy=true and empty (non-null) drift
// arrays, so scripts can gate on `.healthy` without inspecting the report text.
func TestDoctorJSONHealthyStore(t *testing.T) {
	s := newSession(t)
	s.newScratchID("tidy")

	out, err := s.run("doctor", "--json")
	if err != nil {
		t.Fatalf("doctor --json: %v (out=%s)", err, out)
	}

	// The JSON path is pure data: personality and color stay out of it, even on
	// a store that would otherwise print the "doctor is in" flavor line.
	if strings.Contains(out, "\x1b[") {
		t.Errorf("doctor --json must be colorless; got %q", out)
	}
	if strings.Contains(out, "doctor is in") || strings.Contains(out, "frowns") {
		t.Errorf("doctor --json must not carry personality; got %q", out)
	}

	var rec struct {
		Healthy     bool `json:"healthy"`
		LiveCount   int  `json:"liveCount"`
		MorgueCount int  `json:"morgueCount"`
		Orphans     []struct {
			Path string `json:"path"`
		} `json:"orphans"`
		Missing []struct {
			ID string `json:"id"`
		} `json:"missing"`
	}
	if err := json.Unmarshal([]byte(out), &rec); err != nil {
		t.Fatalf("doctor --json is not valid JSON: %v (%q)", err, out)
	}

	if !rec.Healthy {
		t.Errorf("clean store should report healthy=true; got %+v", rec)
	}
	if rec.LiveCount != 1 {
		t.Errorf("liveCount = %d, want 1", rec.LiveCount)
	}
	if len(rec.Orphans) != 0 || len(rec.Missing) != 0 {
		t.Errorf("clean store should have no drift; orphans=%d missing=%d", len(rec.Orphans), len(rec.Missing))
	}
}

// TestDoctorJSONReportsDrift: an orphaned file and a missing-content entry both
// surface through `sp doctor --json`, healthy flips to false, and the run stays
// read-only (the orphan is still on disk afterward).
func TestDoctorJSONReportsDrift(t *testing.T) {
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

	out, err := s.run("doctor", "--json")
	if err != nil {
		t.Fatalf("doctor --json: %v (out=%s)", err, out)
	}

	var rec struct {
		Healthy bool `json:"healthy"`
		Orphans []struct {
			Path      string `json:"path"`
			Area      string `json:"area"`
			Size      int64  `json:"size"`
			SizeHuman string `json:"sizeHuman"`
		} `json:"orphans"`
		Missing []struct {
			ID           string `json:"id"`
			Name         string `json:"name"`
			ExpectedPath string `json:"expectedPath"`
		} `json:"missing"`
	}
	if err := json.Unmarshal([]byte(out), &rec); err != nil {
		t.Fatalf("doctor --json is not valid JSON: %v (%q)", err, out)
	}

	if rec.Healthy {
		t.Errorf("store with orphan + missing must report healthy=false; got %q", out)
	}
	if len(rec.Orphans) != 1 || !strings.HasSuffix(rec.Orphans[0].Path, "deadbeef.md") {
		t.Errorf("expected one orphan ending deadbeef.md; got %#v", rec.Orphans)
	}
	if len(rec.Missing) != 1 || rec.Missing[0].ID != id || rec.Missing[0].Name != "ghost" {
		t.Errorf("expected missing entry for %q/ghost; got %#v", id, rec.Missing)
	}

	// Read-only: doctor must not tidy the orphan it reported.
	if _, err := os.Stat(orphan); err != nil {
		t.Errorf("doctor must not delete the orphan; stat err = %v", err)
	}
}
