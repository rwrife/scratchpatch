package store

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// findOrphan returns the orphan in d whose path ends in name, or fails. Keeps
// the assertions readable when several orphans are present.
func findOrphan(t *testing.T, d Diagnosis, name string) Orphan {
	t.Helper()
	for _, o := range d.Orphans {
		if filepath.Base(o.Path) == name {
			return o
		}
	}
	t.Fatalf("no orphan named %q in %+v", name, d.Orphans)
	return Orphan{}
}

// findMissing returns the missing record in d with the given id, or fails.
func findMissing(t *testing.T, d Diagnosis, id string) Missing {
	t.Helper()
	for _, m := range d.Missing {
		if m.Scratch.ID == id {
			return m
		}
	}
	t.Fatalf("no missing entry with id %q in %+v", id, d.Missing)
	return Missing{}
}

// TestDoctorHealthyStore: a store where every index entry has its file and no
// stray files exist reports healthy, with accurate counts and tracked size.
func TestDoctorHealthyStore(t *testing.T) {
	s, _ := OpenWith(testConfig(t))
	seed(t, s, "alpha", "hello\n")     // 6 bytes, live
	live := seed(t, s, "beta", "hi\n") // 3 bytes, live
	// Soft-delete one so we exercise the morgue path too.
	if _, err := s.MoveToMorgue(live); err != nil {
		t.Fatalf("MoveToMorgue: %v", err)
	}

	d, err := s.Doctor()
	if err != nil {
		t.Fatalf("Doctor: %v", err)
	}

	if !d.Healthy() {
		t.Errorf("store should be healthy, got orphans=%v missing=%v", d.Orphans, d.Missing)
	}
	if d.LiveCount != 1 || d.MorgueCount != 1 {
		t.Errorf("counts: live=%d morgue=%d, want 1/1", d.LiveCount, d.MorgueCount)
	}
	// 6 ("hello\n") + 3 ("hi\n") = 9 bytes of tracked content, spread across
	// scratches/ and morgue/.
	if d.TrackedSize != 9 {
		t.Errorf("TrackedSize = %d, want 9", d.TrackedSize)
	}
	if d.OrphanSize != 0 || d.TotalSize() != 9 {
		t.Errorf("OrphanSize=%d TotalSize=%d, want 0/9", d.OrphanSize, d.TotalSize())
	}
}

// TestDoctorEmptyStoreIsHealthy: a freshly-opened store with nothing in it is
// trivially healthy and reports zeroes (not an error, not "unhealthy").
func TestDoctorEmptyStoreIsHealthy(t *testing.T) {
	s, _ := OpenWith(testConfig(t))
	d, err := s.Doctor()
	if err != nil {
		t.Fatalf("Doctor: %v", err)
	}
	if !d.Healthy() || d.LiveCount != 0 || d.MorgueCount != 0 || d.TotalSize() != 0 {
		t.Errorf("empty store: %+v, want healthy zeroes", d)
	}
}

// TestDoctorDetectsOrphanInBothAreas: content files that no index entry claims —
// in scratches/ and in morgue/ — are reported as orphans, tagged with the right
// area, sized, and summed into OrphanSize.
func TestDoctorDetectsOrphanInBothAreas(t *testing.T) {
	s, _ := OpenWith(testConfig(t))
	cfg := s.Config()

	liveOrphan := filepath.Join(cfg.ScratchesPath(), "cafef00d.md")
	if err := os.WriteFile(liveOrphan, []byte("orphaned\n"), 0o600); err != nil { // 9 bytes
		t.Fatalf("write live orphan: %v", err)
	}
	morgueOrphan := filepath.Join(cfg.MorguePath(), "deadbeef.txt")
	if err := os.WriteFile(morgueOrphan, []byte("ghost\n"), 0o600); err != nil { // 6 bytes
		t.Fatalf("write morgue orphan: %v", err)
	}

	d, err := s.Doctor()
	if err != nil {
		t.Fatalf("Doctor: %v", err)
	}

	if d.Healthy() {
		t.Fatal("store with orphans must not be healthy")
	}
	if len(d.Orphans) != 2 {
		t.Fatalf("want 2 orphans, got %d: %+v", len(d.Orphans), d.Orphans)
	}

	live := findOrphan(t, d, "cafef00d.md")
	if live.Area != "scratches" || live.Size != 9 {
		t.Errorf("live orphan: area=%q size=%d, want scratches/9", live.Area, live.Size)
	}
	morgue := findOrphan(t, d, "deadbeef.txt")
	if morgue.Area != "morgue" || morgue.Size != 6 {
		t.Errorf("morgue orphan: area=%q size=%d, want morgue/6", morgue.Area, morgue.Size)
	}
	if d.OrphanSize != 15 {
		t.Errorf("OrphanSize = %d, want 15", d.OrphanSize)
	}
	// No tracked content exists, so the whole footprint is orphan bytes.
	if d.TrackedSize != 0 || d.TotalSize() != 15 {
		t.Errorf("TrackedSize=%d TotalSize=%d, want 0/15", d.TrackedSize, d.TotalSize())
	}
}

// TestDoctorDetectsMissingContent: an index entry whose content file has been
// removed out from under it is reported as missing, with the path it expected —
// for both a live entry and a morgued one.
func TestDoctorDetectsMissingContent(t *testing.T) {
	s, _ := OpenWith(testConfig(t))

	liveSc := seed(t, s, "vanished", "poof\n")
	if err := os.Remove(s.ContentPath(liveSc)); err != nil {
		t.Fatalf("remove live content: %v", err)
	}

	morgueSc := seed(t, s, "exhumed", "gone\n")
	moved, err := s.MoveToMorgue(morgueSc)
	if err != nil {
		t.Fatalf("MoveToMorgue: %v", err)
	}
	if err := os.Remove(s.LivePath(moved)); err != nil {
		t.Fatalf("remove morgue content: %v", err)
	}

	d, err := s.Doctor()
	if err != nil {
		t.Fatalf("Doctor: %v", err)
	}

	if d.Healthy() {
		t.Fatal("store with missing content must not be healthy")
	}
	if len(d.Missing) != 2 {
		t.Fatalf("want 2 missing, got %d: %+v", len(d.Missing), d.Missing)
	}
	if len(d.Orphans) != 0 {
		t.Errorf("expected no orphans, got %+v", d.Orphans)
	}

	gotLive := findMissing(t, d, liveSc.ID)
	if gotLive.ExpectedPath != s.ContentPath(liveSc) {
		t.Errorf("live ExpectedPath = %q, want %q", gotLive.ExpectedPath, s.ContentPath(liveSc))
	}
	gotMorgue := findMissing(t, d, moved.ID)
	if gotMorgue.ExpectedPath != s.LivePath(moved) {
		t.Errorf("morgue ExpectedPath = %q, want %q", gotMorgue.ExpectedPath, s.LivePath(moved))
	}
	// Missing files contribute nothing to the footprint.
	if d.TotalSize() != 0 {
		t.Errorf("TotalSize = %d, want 0 (both files missing)", d.TotalSize())
	}
}

// TestDoctorIgnoresTransientTempFiles: the .index-*.tmp / .move-*.tmp files the
// atomic writers leave mid-operation are not orphans — a concurrent write must
// not make doctor cry wolf.
func TestDoctorIgnoresTransientTempFiles(t *testing.T) {
	s, _ := OpenWith(testConfig(t))
	cfg := s.Config()

	for _, f := range []string{
		filepath.Join(cfg.Home, ".index-123.json.tmp"),
		filepath.Join(cfg.ScratchesPath(), ".move-abc.tmp"),
		filepath.Join(cfg.MorguePath(), ".move-def.tmp"),
	} {
		if err := os.WriteFile(f, []byte("in-flight\n"), 0o600); err != nil {
			t.Fatalf("write temp %s: %v", f, err)
		}
	}

	d, err := s.Doctor()
	if err != nil {
		t.Fatalf("Doctor: %v", err)
	}
	if !d.Healthy() {
		t.Errorf("transient temp files must not register as orphans, got %+v", d.Orphans)
	}
	if d.OrphanSize != 0 {
		t.Errorf("OrphanSize = %d, want 0 (temps ignored)", d.OrphanSize)
	}
}

// TestDoctorIsReadOnly: running Doctor must not move, delete, or create
// anything — it's a diagnosis, not a treatment. We snapshot both directories
// and the index before and after.
func TestDoctorIsReadOnly(t *testing.T) {
	s, _ := OpenWith(testConfig(t))
	cfg := s.Config()

	seed(t, s, "keep", "stay\n")
	// An orphan and a missing entry, so doctor has something to find and might
	// be tempted to "fix".
	orphan := filepath.Join(cfg.ScratchesPath(), "0badc0de.md")
	if err := os.WriteFile(orphan, []byte("loose\n"), 0o600); err != nil {
		t.Fatalf("write orphan: %v", err)
	}
	gone := seed(t, s, "ghost", "boo\n")
	if err := os.Remove(s.ContentPath(gone)); err != nil {
		t.Fatalf("remove content: %v", err)
	}

	before := snapshot(t, cfg.Home)

	if _, err := s.Doctor(); err != nil {
		t.Fatalf("Doctor: %v", err)
	}

	after := snapshot(t, cfg.Home)
	if before != after {
		t.Errorf("Doctor changed the store.\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

// snapshot returns a stable, newline-joined listing of every regular file under
// root with its size, so two snapshots compare equal iff nothing was added,
// removed, or resized.
func snapshot(t *testing.T, root string) string {
	t.Helper()
	var lines []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		lines = append(lines, rel+"\t"+strconv.FormatInt(info.Size(), 10))
		return nil
	})
	if err != nil {
		t.Fatalf("snapshot walk: %v", err)
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n")
}
