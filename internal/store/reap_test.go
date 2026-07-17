package store

import (
	"errors"
	"os"
	"testing"
	"time"

	"github.com/rwrife/scratchpatch/internal/index"
)

// reapNow is a fixed clock for the reap tests so every boundary is exact.
var reapNow = time.Date(2026, 6, 28, 12, 0, 0, 0, time.UTC)

// seedExpired creates a live scratch whose ExpiresAt is forced to expAt (so we
// can make it already-expired relative to a test clock) and writes body to it.
func seedExpired(t *testing.T, s *Store, name string, expAt time.Time, body string) index.Scratch {
	t.Helper()
	sc := seed(t, s, name, body)
	sc.ExpiresAt = expAt
	if err := s.Index().Put(sc); err != nil {
		t.Fatalf("Put expired: %v", err)
	}
	got, _ := s.Index().Get(sc.ID)
	return got
}

// seedMorgued creates a scratch, soft-deletes it, then forces its DeletedAt to
// delAt so its purge deadline (delAt+grace) lands where the test wants.
func seedMorgued(t *testing.T, s *Store, name string, delAt time.Time, body string) index.Scratch {
	t.Helper()
	sc := seed(t, s, name, body)
	moved, err := s.MoveToMorgue(sc)
	if err != nil {
		t.Fatalf("MoveToMorgue(%q): %v", name, err)
	}
	moved.DeletedAt = &delAt
	if err := s.Index().Put(moved); err != nil {
		t.Fatalf("Put morgued: %v", err)
	}
	got, _ := s.Index().Get(moved.ID)
	return got
}

func TestHardDeleteRemovesContentAndIndex(t *testing.T) {
	s, _ := OpenWith(testConfig(t))
	// Deleted 4 days ago; grace is 3d, so it's past purge at reapNow.
	sc := seedMorgued(t, s, "ash", reapNow.Add(-4*24*time.Hour), "dust\n")
	morguePath := s.morguePath(sc)

	if err := s.HardDelete(sc, reapNow); err != nil {
		t.Fatalf("HardDelete: %v", err)
	}
	if _, err := os.Stat(morguePath); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("morgue content should be gone, stat err = %v", err)
	}
	if _, err := s.Index().Get(sc.ID); !errors.Is(err, index.ErrNotFound) {
		t.Errorf("index entry should be gone, Get err = %v", err)
	}
}

func TestHardDeleteRefusesLiveScratch(t *testing.T) {
	s, _ := OpenWith(testConfig(t))
	sc := seed(t, s, "alive", "still here\n")
	if err := s.HardDelete(sc, reapNow); err == nil {
		t.Fatal("HardDelete on a live scratch must error")
	}
	// And it must not have touched the content.
	if _, err := os.Stat(s.ContentPath(sc)); err != nil {
		t.Errorf("live content must be untouched, stat err = %v", err)
	}
}

func TestHardDeleteRefusesWithinGrace(t *testing.T) {
	s, _ := OpenWith(testConfig(t))
	// Deleted 1 day ago; grace 3d → still 2 days of grace left.
	sc := seedMorgued(t, s, "fresh-corpse", reapNow.Add(-24*time.Hour), "wait\n")
	morguePath := s.morguePath(sc)

	if err := s.HardDelete(sc, reapNow); err == nil {
		t.Fatal("HardDelete within the grace window must error")
	}
	if _, err := os.Stat(morguePath); err != nil {
		t.Errorf("in-grace content must be untouched, stat err = %v", err)
	}
	if _, err := s.Index().Get(sc.ID); err != nil {
		t.Errorf("in-grace index entry must survive, Get err = %v", err)
	}
}

func TestHardDeleteToleratesMissingContent(t *testing.T) {
	s, _ := OpenWith(testConfig(t))
	sc := seedMorgued(t, s, "orphan", reapNow.Add(-4*24*time.Hour), "")
	// Simulate content already gone (e.g. a prior half-purge).
	_ = os.Remove(s.morguePath(sc))
	if err := s.HardDelete(sc, reapNow); err != nil {
		t.Fatalf("HardDelete should tolerate missing content: %v", err)
	}
	if _, err := s.Index().Get(sc.ID); !errors.Is(err, index.ErrNotFound) {
		t.Errorf("index entry should still be cleaned up, Get err = %v", err)
	}
}

func TestReapSweepsExpiredAndPurgesPastGrace(t *testing.T) {
	s, _ := OpenWith(testConfig(t))

	// Stage-1 candidate: live but expired an hour ago.
	expired := seedExpired(t, s, "expired-live", reapNow.Add(-time.Hour), "reap me\n")
	// Stage-1 non-candidate: live and fresh (expires in 2 days).
	fresh := seedExpired(t, s, "still-fresh", reapNow.Add(48*time.Hour), "keep me\n")
	// Stage-2 candidate: morgued 4 days ago (past 3d grace).
	doomed := seedMorgued(t, s, "past-grace", reapNow.Add(-4*24*time.Hour), "purge me\n")
	// Stage-2 non-candidate: morgued 1 day ago (still in grace).
	lingering := seedMorgued(t, s, "in-grace", reapNow.Add(-24*time.Hour), "not yet\n")

	doomedPath := s.morguePath(doomed)

	plan, err := s.Reap(reapNow, false)
	if err != nil {
		t.Fatalf("Reap: %v", err)
	}

	if len(plan.Morgued) != 1 || plan.Morgued[0].ID != expired.ID {
		t.Errorf("Morgued = %v, want just %s", ids(plan.Morgued), expired.ID)
	}
	if len(plan.Purged) != 1 || plan.Purged[0].ID != doomed.ID {
		t.Errorf("Purged = %v, want just %s", ids(plan.Purged), doomed.ID)
	}

	// The expired scratch is now in the morgue, not live.
	if got, _ := s.Index().Get(expired.ID); !got.Morgued() {
		t.Error("expired scratch should now be morgued")
	}
	// The fresh scratch is untouched and still live.
	if got, _ := s.Index().Get(fresh.ID); !got.Live() {
		t.Error("fresh scratch should remain live")
	}
	// The doomed scratch is gone from disk and index.
	if _, err := os.Stat(doomedPath); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("purged content should be gone, stat err = %v", err)
	}
	if _, err := s.Index().Get(doomed.ID); !errors.Is(err, index.ErrNotFound) {
		t.Errorf("purged index entry should be gone, err = %v", err)
	}
	// The lingering morgue item survives this reap.
	if _, err := s.Index().Get(lingering.ID); err != nil {
		t.Errorf("in-grace morgue item must survive, err = %v", err)
	}
}

// TestReapNeverPurgesWhatItJustSwept is the single-step-safety guarantee: a
// scratch swept into the morgue this pass must NOT be hard-deleted in the same
// pass, even if its (already long-past) expiry would, on a naive read, look
// past grace. Its grace clock starts at the sweep, i.e. now.
func TestReapNeverPurgesWhatItJustSwept(t *testing.T) {
	s, _ := OpenWith(testConfig(t))
	// Expired ages ago — but it's still LIVE, so this reap can only morgue it.
	old := seedExpired(t, s, "ancient", reapNow.Add(-30*24*time.Hour), "old\n")

	plan, err := s.Reap(reapNow, false)
	if err != nil {
		t.Fatalf("Reap: %v", err)
	}
	if len(plan.Purged) != 0 {
		t.Errorf("nothing should be purged in the same pass it was swept, got %v", ids(plan.Purged))
	}
	if len(plan.Morgued) != 1 || plan.Morgued[0].ID != old.ID {
		t.Fatalf("Morgued = %v, want just %s", ids(plan.Morgued), old.ID)
	}
	// Content must still exist in the morgue (moved, not purged).
	got, _ := s.Index().Get(old.ID)
	if !got.Morgued() {
		t.Fatal("scratch should be morgued, not purged")
	}
	if _, err := os.Stat(s.morguePath(got)); err != nil {
		t.Errorf("swept content must survive in the morgue, stat err = %v", err)
	}
}

func TestReapDryRunChangesNothing(t *testing.T) {
	s, _ := OpenWith(testConfig(t))
	expired := seedExpired(t, s, "expired-live", reapNow.Add(-time.Hour), "x\n")
	doomed := seedMorgued(t, s, "past-grace", reapNow.Add(-4*24*time.Hour), "y\n")

	expiredLivePath := s.ContentPath(expired)
	doomedPath := s.morguePath(doomed)

	plan, err := s.Reap(reapNow, true)
	if err != nil {
		t.Fatalf("Reap dry-run: %v", err)
	}
	if !plan.DryRun {
		t.Error("plan.DryRun should be true")
	}

	// The plan still reports exactly what *would* happen.
	if len(plan.Morgued) != 1 || plan.Morgued[0].ID != expired.ID {
		t.Errorf("dry-run Morgued = %v, want %s", ids(plan.Morgued), expired.ID)
	}
	if len(plan.Purged) != 1 || plan.Purged[0].ID != doomed.ID {
		t.Errorf("dry-run Purged = %v, want %s", ids(plan.Purged), doomed.ID)
	}

	// But nothing on disk or in the index changed.
	if _, err := os.Stat(expiredLivePath); err != nil {
		t.Errorf("dry-run must leave the expired scratch live on disk, err = %v", err)
	}
	if got, _ := s.Index().Get(expired.ID); !got.Live() {
		t.Error("dry-run must not morgue the expired scratch")
	}
	if _, err := os.Stat(doomedPath); err != nil {
		t.Errorf("dry-run must leave the doomed content on disk, err = %v", err)
	}
	if _, err := s.Index().Get(doomed.ID); err != nil {
		t.Errorf("dry-run must not drop the doomed index entry, err = %v", err)
	}
}

func TestReapEmptyWhenNothingDue(t *testing.T) {
	s, _ := OpenWith(testConfig(t))
	seedExpired(t, s, "fresh", reapNow.Add(48*time.Hour), "")     // live, fresh
	seedMorgued(t, s, "in-grace", reapNow.Add(-24*time.Hour), "") // morgued, in grace

	plan, err := s.Reap(reapNow, false)
	if err != nil {
		t.Fatalf("Reap: %v", err)
	}
	if !plan.Empty() {
		t.Errorf("expected an empty reap, got morgued=%v purged=%v",
			ids(plan.Morgued), ids(plan.Purged))
	}
}

// ids extracts the ids of a scratch slice for compact failure messages.
func ids(scs []index.Scratch) []string {
	out := make([]string, len(scs))
	for i, sc := range scs {
		out[i] = sc.ID
	}
	return out
}

// TestReapSkipsPinnedAndCountsThem verifies an expired but pinned scratch is
// left live and tallied in PinnedSkipped rather than swept to the morgue.
func TestReapSkipsPinnedAndCountsThem(t *testing.T) {
	s, _ := OpenWith(testConfig(t))
	// Two expired live scratches; pin one.
	pinned := seedExpired(t, s, "keep-me", reapNow.Add(-time.Hour), "important\n")
	seedExpired(t, s, "sweep-me", reapNow.Add(-time.Hour), "whatever\n")

	if _, err := s.SetPin(pinned.ID, true); err != nil {
		t.Fatalf("SetPin: %v", err)
	}

	plan, err := s.Reap(reapNow, false)
	if err != nil {
		t.Fatalf("Reap: %v", err)
	}
	if plan.PinnedSkipped != 1 {
		t.Errorf("PinnedSkipped = %d, want 1", plan.PinnedSkipped)
	}
	if len(plan.Morgued) != 1 {
		t.Fatalf("Morgued = %d, want 1", len(plan.Morgued))
	}
	if plan.Morgued[0].ID == pinned.ID {
		t.Error("pinned scratch was swept to the morgue")
	}

	// The pinned scratch must still be live after the reap.
	got, err := s.Index().Get(pinned.ID)
	if err != nil {
		t.Fatalf("Get pinned: %v", err)
	}
	if !got.Live() {
		t.Error("pinned scratch should still be live")
	}
	if !got.Pinned {
		t.Error("pinned flag should persist through reap")
	}
}

// TestSetPinRoundTripsThroughIndex verifies pin state persists and unpins cleanly.
func TestSetPinRoundTripsThroughIndex(t *testing.T) {
	s, _ := OpenWith(testConfig(t))
	sc := seed(t, s, "note", "body\n")

	updated, err := s.SetPin(sc.ID, true)
	if err != nil {
		t.Fatalf("SetPin true: %v", err)
	}
	if !updated.Pinned {
		t.Error("returned record should be pinned")
	}
	got, _ := s.Index().Get(sc.ID)
	if !got.Pinned {
		t.Error("pin should persist in the index")
	}

	if _, err := s.SetPin(sc.ID, false); err != nil {
		t.Fatalf("SetPin false: %v", err)
	}
	got, _ = s.Index().Get(sc.ID)
	if got.Pinned {
		t.Error("unpin should clear the flag in the index")
	}
}

// TestReapDryRunSkipsPinned verifies dry-run also exempts pinned scratches.
func TestReapDryRunSkipsPinned(t *testing.T) {
	s, _ := OpenWith(testConfig(t))
	pinned := seedExpired(t, s, "keep", reapNow.Add(-time.Hour), "x\n")
	if _, err := s.SetPin(pinned.ID, true); err != nil {
		t.Fatalf("SetPin: %v", err)
	}
	plan, err := s.Reap(reapNow, true)
	if err != nil {
		t.Fatalf("Reap dry-run: %v", err)
	}
	if plan.PinnedSkipped != 1 || len(plan.Morgued) != 0 {
		t.Errorf("dry-run: PinnedSkipped=%d Morgued=%d, want 1 and 0", plan.PinnedSkipped, len(plan.Morgued))
	}
}
