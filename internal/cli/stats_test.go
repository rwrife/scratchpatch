package cli

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestStatsEmptyStore: a fresh store gives a friendly zero-state, not a crash
// or a wall of zeros.
func TestStatsEmptyStore(t *testing.T) {
	s := newSession(t)
	out, err := s.run("stats", "--no-color")
	if err != nil {
		t.Fatalf("stats: %v (out=%s)", err, out)
	}
	if !strings.Contains(out, "empty") {
		t.Errorf("empty store should report a zero-state; got %q", out)
	}
}

// TestStatsCountsLiveScratches: a couple of live scratches show up in the
// living line and the footprint.
func TestStatsCountsLiveScratches(t *testing.T) {
	s := newSession(t)
	s.newScratchID("one")
	s.newScratchID("two")

	out, err := s.run("stats", "--no-color")
	if err != nil {
		t.Fatalf("stats: %v (out=%s)", err, out)
	}
	if !strings.Contains(out, "2 scratches") {
		t.Errorf("stats should count both live scratches; got %q", out)
	}
	if !strings.Contains(out, "oldest survivor") {
		t.Errorf("stats should name an oldest survivor; got %q", out)
	}
}

// TestStatsJSON: --json emits a stable object with the expected fields and no
// color.
func TestStatsJSON(t *testing.T) {
	s := newSession(t)
	s.newScratchID("solo")

	out, err := s.run("stats", "--json")
	if err != nil {
		t.Fatalf("stats --json: %v (out=%s)", err, out)
	}
	if strings.Contains(out, "\x1b[") {
		t.Errorf("--json must be color-free; got %q", out)
	}
	var got struct {
		LiveCount  int `json:"liveCount"`
		TotalBytes int64 `json:"totalBytes"`
		Oldest     *struct {
			ID string `json:"id"`
		} `json:"oldest"`
		Tags []any `json:"tags"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, out)
	}
	if got.LiveCount != 1 {
		t.Errorf("LiveCount = %d, want 1", got.LiveCount)
	}
	if got.Oldest == nil || got.Oldest.ID == "" {
		t.Errorf("oldest survivor should be present; got %+v", got.Oldest)
	}
	if got.Tags == nil {
		t.Errorf("tags should serialize as [] not null")
	}
}
