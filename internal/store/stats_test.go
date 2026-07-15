package store

import (
	"testing"
	"time"

	"github.com/rwrife/scratchpatch/internal/index"
)

// seedTagged creates a live scratch with the given tags and forces its
// CreatedAt so oldest-survivor selection is deterministic.
func seedTagged(t *testing.T, s *Store, name string, created time.Time, tags []string) index.Scratch {
	t.Helper()
	sc, _, err := s.Create(CreateOptions{Name: name, Ext: "txt", Tags: tags})
	if err != nil {
		t.Fatalf("Create(%q): %v", name, err)
	}
	sc.CreatedAt = created
	sc.Size = 100
	if err := s.Index().Put(sc); err != nil {
		t.Fatalf("Put: %v", err)
	}
	got, _ := s.Index().Get(sc.ID)
	return got
}

func TestStatsEmptyStore(t *testing.T) {
	s, _ := OpenWith(testConfig(t))
	st, err := s.Stats()
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if st.LiveCount != 0 || st.MorgueCount != 0 || st.TotalBytes != 0 {
		t.Errorf("empty store should be all zero, got %+v", st)
	}
	if st.OldestID != "" {
		t.Errorf("empty store should have no oldest survivor, got %q", st.OldestID)
	}
	if len(st.Tags) != 0 {
		t.Errorf("empty store should have no tags, got %v", st.Tags)
	}
	if st.Grace != s.Config().Grace {
		t.Errorf("Grace = %v, want %v", st.Grace, s.Config().Grace)
	}
}

func TestStatsMixedLiveAndMorgue(t *testing.T) {
	s, _ := OpenWith(testConfig(t))
	now := time.Now()

	// Three live scratches; middle one is the oldest survivor.
	seedTagged(t, s, "young", now.Add(-1*time.Hour), []string{"a"})
	oldest := seedTagged(t, s, "old", now.Add(-100*time.Hour), []string{"a", "b"})
	seedTagged(t, s, "mid", now.Add(-10*time.Hour), []string{"b"})

	// A morgue item well past grace (purgeable) and one still within grace.
	del1 := now.Add(-10 * 24 * time.Hour)
	seedMorgued(t, s, "purgeable", del1, "x\n")
	del2 := now.Add(-1 * time.Hour)
	seedMorgued(t, s, "fresh-dead", del2, "y\n")

	st, err := s.Stats()
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}

	if st.LiveCount != 3 {
		t.Errorf("LiveCount = %d, want 3", st.LiveCount)
	}
	if st.MorgueCount != 2 {
		t.Errorf("MorgueCount = %d, want 2", st.MorgueCount)
	}
	if st.PurgeableCount != 1 {
		t.Errorf("PurgeableCount = %d, want 1", st.PurgeableCount)
	}
	if st.LiveBytes != 300 {
		t.Errorf("LiveBytes = %d, want 300", st.LiveBytes)
	}
	if st.TotalBytes != st.LiveBytes+st.MorgueBytes {
		t.Errorf("TotalBytes = %d, want %d", st.TotalBytes, st.LiveBytes+st.MorgueBytes)
	}
	if st.OldestID != oldest.ID {
		t.Errorf("OldestID = %q, want %q (the oldest survivor)", st.OldestID, oldest.ID)
	}
	if st.OldestName != "old" {
		t.Errorf("OldestName = %q, want %q", st.OldestName, "old")
	}
	if st.OldestAge < 90*time.Hour {
		t.Errorf("OldestAge = %v, want ~100h", st.OldestAge)
	}
}

func TestStatsTagBreakdownOrdered(t *testing.T) {
	s, _ := OpenWith(testConfig(t))
	now := time.Now()
	// "a" appears 3x, "b" 2x, "c" 1x → expect that ranking.
	seedTagged(t, s, "s1", now.Add(-1*time.Hour), []string{"a", "b", "c"})
	seedTagged(t, s, "s2", now.Add(-2*time.Hour), []string{"a", "b"})
	seedTagged(t, s, "s3", now.Add(-3*time.Hour), []string{"a"})

	st, err := s.Stats()
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if len(st.Tags) != 3 {
		t.Fatalf("Tags len = %d, want 3: %+v", len(st.Tags), st.Tags)
	}
	want := []TagCount{{"a", 3}, {"b", 2}, {"c", 1}}
	for i, w := range want {
		if st.Tags[i] != w {
			t.Errorf("Tags[%d] = %+v, want %+v", i, st.Tags[i], w)
		}
	}
}

func TestStatsMorgueScratchesNotTagged(t *testing.T) {
	s, _ := OpenWith(testConfig(t))
	now := time.Now()
	// Live scratch with a tag; a morgued one whose tag must NOT count toward
	// the (living) breakdown.
	seedTagged(t, s, "live", now.Add(-1*time.Hour), []string{"keep"})
	m := seedTagged(t, s, "dead", now.Add(-2*time.Hour), []string{"gone"})
	if _, err := s.MoveToMorgue(m); err != nil {
		t.Fatalf("MoveToMorgue: %v", err)
	}

	st, err := s.Stats()
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if st.LiveCount != 1 || st.MorgueCount != 1 {
		t.Fatalf("counts = live %d / morgue %d, want 1/1", st.LiveCount, st.MorgueCount)
	}
	if len(st.Tags) != 1 || st.Tags[0].Tag != "keep" {
		t.Errorf("only living tags should count, got %+v", st.Tags)
	}
	// Oldest survivor is the live one, since the morgued scratch is excluded.
	live, _ := s.Index().List()
	_ = live
	if st.OldestName != "live" {
		t.Errorf("OldestName = %q, want %q", st.OldestName, "live")
	}
}
