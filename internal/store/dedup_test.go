package store

import (
	"os"
	"testing"
	"time"

	"github.com/rwrife/scratchpatch/internal/index"
)

// seedAt is like seed but stamps a specific CreatedAt so tests can control
// which member of a duplicate cluster is the oldest (canonical). It writes the
// content, then rewrites the index record's CreatedAt.
func seedAt(t *testing.T, s *Store, name, body string, created time.Time) index.Scratch {
	t.Helper()
	sc := seed(t, s, name, body)
	sc.CreatedAt = created
	if err := s.Index().Put(sc); err != nil {
		t.Fatalf("Put(%q): %v", name, err)
	}
	return sc
}

// TestDedupNoDuplicates: a store where every scratch has unique content reports
// clean, with the scanned count set and no clusters.
func TestDedupNoDuplicates(t *testing.T) {
	s, _ := OpenWith(testConfig(t))
	seed(t, s, "alpha", "one\n")
	seed(t, s, "beta", "two\n")
	seed(t, s, "gamma", "three\n")

	r, err := s.Dedup()
	if err != nil {
		t.Fatalf("Dedup: %v", err)
	}
	if !r.Clean() {
		t.Errorf("expected clean, got clusters=%+v", r.Clusters)
	}
	if r.ScannedCount != 3 {
		t.Errorf("scanned=%d, want 3", r.ScannedCount)
	}
	if r.TotalWasted != 0 {
		t.Errorf("wasted=%d, want 0", r.TotalWasted)
	}
}

// TestDedupEmptyStore: an empty store is trivially clean.
func TestDedupEmptyStore(t *testing.T) {
	s, _ := OpenWith(testConfig(t))
	r, err := s.Dedup()
	if err != nil {
		t.Fatalf("Dedup: %v", err)
	}
	if !r.Clean() || r.ScannedCount != 0 {
		t.Errorf("empty store: clean=%v scanned=%d, want true/0", r.Clean(), r.ScannedCount)
	}
}

// TestDedupSingleScratch: one scratch can never be a duplicate of anything.
func TestDedupSingleScratch(t *testing.T) {
	s, _ := OpenWith(testConfig(t))
	seed(t, s, "solo", "lonely\n")
	r, err := s.Dedup()
	if err != nil {
		t.Fatalf("Dedup: %v", err)
	}
	if !r.Clean() {
		t.Errorf("single scratch should be clean, got %+v", r.Clusters)
	}
}

// TestDedupExactDuplicates: identical content clusters together, the oldest is
// canonical, and wasted bytes count only the redundant copies. Different names
// with identical bytes are still a duplicate.
func TestDedupExactDuplicates(t *testing.T) {
	s, _ := OpenWith(testConfig(t))
	base := time.Now().Add(-time.Hour)
	body := "duplicate me\n" // 13 bytes

	old := seedAt(t, s, "original", body, base)                      // oldest → canonical
	mid := seedAt(t, s, "copy-a", body, base.Add(10*time.Minute))    // redundant
	newest := seedAt(t, s, "copy-b", body, base.Add(20*time.Minute)) // redundant
	seed(t, s, "unique", "not a dup\n")                              // control

	r, err := s.Dedup()
	if err != nil {
		t.Fatalf("Dedup: %v", err)
	}
	if len(r.Clusters) != 1 {
		t.Fatalf("clusters=%d, want 1: %+v", len(r.Clusters), r.Clusters)
	}
	c := r.Clusters[0]
	if len(c.Members) != 3 {
		t.Fatalf("members=%d, want 3", len(c.Members))
	}
	if c.Canonical().ID != old.ID {
		t.Errorf("canonical=%s, want oldest %s", c.Canonical().ID, old.ID)
	}
	if !c.Members[0].Canonical || c.Members[1].Canonical || c.Members[2].Canonical {
		t.Errorf("only Members[0] should be canonical: %+v", c.Members)
	}
	// Two redundant copies of 13 bytes each = 26 wasted.
	wantWasted := int64(len(body) * 2)
	if c.WastedBytes != wantWasted {
		t.Errorf("wasted=%d, want %d", c.WastedBytes, wantWasted)
	}
	if r.TotalWasted != wantWasted {
		t.Errorf("total wasted=%d, want %d", r.TotalWasted, wantWasted)
	}
	// Redundant copies are the two non-oldest ids.
	redundant := map[string]bool{}
	for _, m := range c.Redundant() {
		redundant[m.Scratch.ID] = true
	}
	if !redundant[mid.ID] || !redundant[newest.ID] {
		t.Errorf("redundant set = %v, want %s and %s", redundant, mid.ID, newest.ID)
	}
}

// TestDedupSkipsUnreadableContent: a scratch whose content file is gone is
// skipped (not counted, not crashed), and the remaining real duplicates still
// cluster.
func TestDedupSkipsUnreadableContent(t *testing.T) {
	s, _ := OpenWith(testConfig(t))
	body := "shared\n"
	seedAt(t, s, "a", body, time.Now().Add(-time.Hour))
	seedAt(t, s, "b", body, time.Now().Add(-30*time.Minute))
	broken := seed(t, s, "broken", "orphaned\n")

	// Remove the content file out from under the index to simulate drift.
	if err := os.Remove(s.LivePath(broken)); err != nil {
		t.Fatalf("remove content: %v", err)
	}

	r, err := s.Dedup()
	if err != nil {
		t.Fatalf("Dedup: %v", err)
	}
	if r.ScannedCount != 2 {
		t.Errorf("scanned=%d, want 2 (broken skipped)", r.ScannedCount)
	}
	if len(r.Clusters) != 1 {
		t.Fatalf("clusters=%d, want 1", len(r.Clusters))
	}
}

// TestDedupReadOnly: the default Dedup path moves nothing — the live set is
// unchanged and nothing is morgued.
func TestDedupReadOnly(t *testing.T) {
	s, _ := OpenWith(testConfig(t))
	body := "keep both live\n"
	seedAt(t, s, "a", body, time.Now().Add(-time.Hour))
	seedAt(t, s, "b", body, time.Now().Add(-time.Minute))

	if _, err := s.Dedup(); err != nil {
		t.Fatalf("Dedup: %v", err)
	}

	live, err := s.ListLive()
	if err != nil {
		t.Fatalf("ListLive: %v", err)
	}
	if len(live) != 2 {
		t.Errorf("live=%d after read-only dedup, want 2", len(live))
	}
	morgue, err := s.ListMorgue()
	if err != nil {
		t.Fatalf("ListMorgue: %v", err)
	}
	if len(morgue) != 0 {
		t.Errorf("morgue=%d after read-only dedup, want 0", len(morgue))
	}
}

// TestCollapseMovesRedundantToMorgue: --collapse morgues the redundant copies,
// keeps the canonical live, never hard-deletes, and reports what moved.
func TestCollapseMovesRedundantToMorgue(t *testing.T) {
	s, _ := OpenWith(testConfig(t))
	base := time.Now().Add(-time.Hour)
	body := "collapse me\n" // 12 bytes

	canonical := seedAt(t, s, "keep", body, base)
	redA := seedAt(t, s, "dup-a", body, base.Add(time.Minute))
	redB := seedAt(t, s, "dup-b", body, base.Add(2*time.Minute))

	report, err := s.Dedup()
	if err != nil {
		t.Fatalf("Dedup: %v", err)
	}
	res, err := s.Collapse(report)
	if err != nil {
		t.Fatalf("Collapse: %v", err)
	}

	if len(res.MovedIDs) != 2 {
		t.Fatalf("moved=%d, want 2: %v", len(res.MovedIDs), res.MovedIDs)
	}
	moved := map[string]bool{res.MovedIDs[0]: true, res.MovedIDs[1]: true}
	if !moved[redA.ID] || !moved[redB.ID] {
		t.Errorf("moved=%v, want %s and %s", res.MovedIDs, redA.ID, redB.ID)
	}
	if moved[canonical.ID] {
		t.Errorf("canonical %s should not have moved", canonical.ID)
	}
	if res.ReclaimedBytes != int64(len(body)*2) {
		t.Errorf("reclaimed=%d, want %d", res.ReclaimedBytes, len(body)*2)
	}

	// Canonical stays live; redundant copies are morgued, not gone.
	live, _ := s.ListLive()
	if len(live) != 1 || live[0].ID != canonical.ID {
		t.Errorf("live set = %+v, want just %s", live, canonical.ID)
	}
	morgue, _ := s.ListMorgue()
	if len(morgue) != 2 {
		t.Errorf("morgue=%d, want 2 (nothing hard-deleted)", len(morgue))
	}
	// Content is recoverable: the morgued files still exist on disk.
	for _, m := range morgue {
		if _, err := os.Stat(s.LivePath(m)); err != nil {
			t.Errorf("morgued content %s missing: %v", m.ID, err)
		}
	}
}
