package store

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rwrife/scratchpatch/internal/index"
)

// seed creates a scratch with the given name and writes body into its content
// file, returning the persisted record. Helper for the lifecycle tests below.
func seed(t *testing.T, s *Store, name, body string) index.Scratch {
	t.Helper()
	sc, path, err := s.Create(CreateOptions{Name: name, Ext: "txt"})
	if err != nil {
		t.Fatalf("Create(%q): %v", name, err)
	}
	if body != "" {
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatalf("write content: %v", err)
		}
		if _, err := s.Touch(sc.ID); err != nil {
			t.Fatalf("Touch: %v", err)
		}
		sc, _ = s.Index().Get(sc.ID)
	}
	return sc
}

func TestMoveToMorgueRelocatesContentAndStampsDeleted(t *testing.T) {
	s, _ := OpenWith(testConfig(t))
	sc := seed(t, s, "doomed", "bye\n")

	livePath := s.ContentPath(sc)
	morguePath := s.morguePath(sc)

	moved, err := s.MoveToMorgue(sc)
	if err != nil {
		t.Fatalf("MoveToMorgue: %v", err)
	}

	if moved.DeletedAt == nil {
		t.Fatal("DeletedAt should be set after soft-delete")
	}
	if !moved.Morgued() {
		t.Error("moved scratch should report Morgued()")
	}

	// Content left scratches/ and arrived in morgue/.
	if _, err := os.Stat(livePath); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("live content should be gone, stat err = %v", err)
	}
	body, err := os.ReadFile(morguePath)
	if err != nil {
		t.Fatalf("read morgue content: %v", err)
	}
	if string(body) != "bye\n" {
		t.Errorf("morgue content = %q, want %q", body, "bye\n")
	}

	// The index reflects the soft-delete: not in live, present in morgue.
	live, _ := s.ListLive()
	if len(live) != 0 {
		t.Errorf("live list should be empty, got %d", len(live))
	}
	dead, _ := s.ListMorgue()
	if len(dead) != 1 || dead[0].ID != sc.ID {
		t.Errorf("morgue list = %+v, want just %s", dead, sc.ID)
	}
}

func TestMoveToMorgueTwiceFails(t *testing.T) {
	s, _ := OpenWith(testConfig(t))
	sc := seed(t, s, "once", "")
	moved, err := s.MoveToMorgue(sc)
	if err != nil {
		t.Fatalf("first MoveToMorgue: %v", err)
	}
	if _, err := s.MoveToMorgue(moved); err == nil {
		t.Error("morguing an already-morgued scratch should error")
	}
}

func TestResurrectRestoresContentAndClearsDeleted(t *testing.T) {
	s, _ := OpenWith(testConfig(t))
	sc := seed(t, s, "phoenix", "rise\n")

	moved, err := s.MoveToMorgue(sc)
	if err != nil {
		t.Fatalf("MoveToMorgue: %v", err)
	}

	restored, err := s.Resurrect(moved)
	if err != nil {
		t.Fatalf("Resurrect: %v", err)
	}
	if restored.DeletedAt != nil {
		t.Error("DeletedAt should be cleared after resurrect")
	}
	if !restored.Live() {
		t.Error("resurrected scratch should report Live()")
	}

	// Content is back under scratches/ and gone from morgue/.
	livePath := s.ContentPath(restored)
	body, err := os.ReadFile(livePath)
	if err != nil {
		t.Fatalf("read restored content: %v", err)
	}
	if string(body) != "rise\n" {
		t.Errorf("restored content = %q, want %q", body, "rise\n")
	}
	if _, err := os.Stat(s.morguePath(restored)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("morgue content should be gone, stat err = %v", err)
	}

	live, _ := s.ListLive()
	if len(live) != 1 {
		t.Errorf("live list should have the resurrected scratch, got %d", len(live))
	}
	dead, _ := s.ListMorgue()
	if len(dead) != 0 {
		t.Errorf("morgue should be empty after resurrect, got %d", len(dead))
	}
}

func TestResurrectLiveScratchFails(t *testing.T) {
	s, _ := OpenWith(testConfig(t))
	sc := seed(t, s, "alive", "")
	if _, err := s.Resurrect(sc); err == nil {
		t.Error("resurrecting a live scratch should error")
	}
}

func TestResolveExactAndPrefix(t *testing.T) {
	s, _ := OpenWith(testConfig(t))
	sc := seed(t, s, "target", "")

	// Exact id.
	got, err := s.Resolve(sc.ID)
	if err != nil {
		t.Fatalf("Resolve(exact): %v", err)
	}
	if got.ID != sc.ID {
		t.Errorf("exact resolve = %s, want %s", got.ID, sc.ID)
	}

	// Unambiguous prefix (first 4 chars of the 8-char id).
	got, err = s.Resolve(sc.ID[:4])
	if err != nil {
		t.Fatalf("Resolve(prefix): %v", err)
	}
	if got.ID != sc.ID {
		t.Errorf("prefix resolve = %s, want %s", got.ID, sc.ID)
	}
}

func TestResolveMissingAndEmpty(t *testing.T) {
	s, _ := OpenWith(testConfig(t))
	if _, err := s.Resolve("deadbeef"); !errors.Is(err, index.ErrNotFound) {
		t.Errorf("missing id should be ErrNotFound, got %v", err)
	}
	if _, err := s.Resolve("  "); !errors.Is(err, index.ErrNotFound) {
		t.Errorf("empty ref should be ErrNotFound, got %v", err)
	}
}

func TestResolveAmbiguousPrefix(t *testing.T) {
	s, _ := OpenWith(testConfig(t))
	// Hand-craft two scratches that share an id prefix so the test is
	// deterministic (random ids would only rarely collide).
	a := index.Scratch{ID: "abc11111", Name: "a", CreatedAt: time.Now(), Ext: "txt"}
	b := index.Scratch{ID: "abc22222", Name: "b", CreatedAt: time.Now(), Ext: "txt"}
	if err := s.Index().Put(a); err != nil {
		t.Fatalf("put a: %v", err)
	}
	if err := s.Index().Put(b); err != nil {
		t.Fatalf("put b: %v", err)
	}

	if _, err := s.Resolve("abc"); !errors.Is(err, ErrAmbiguousID) {
		t.Errorf("ambiguous prefix should be ErrAmbiguousID, got %v", err)
	}
	// But the full id still resolves uniquely.
	got, err := s.Resolve("abc11111")
	if err != nil || got.ID != "abc11111" {
		t.Errorf("exact id of ambiguous-prefix set should resolve, got %s err=%v", got.ID, err)
	}
}

func TestReadContentFromLiveAndMorgue(t *testing.T) {
	s, _ := OpenWith(testConfig(t))
	sc := seed(t, s, "readable", "payload\n")

	// Live read.
	b, err := s.ReadContent(sc)
	if err != nil {
		t.Fatalf("ReadContent(live): %v", err)
	}
	if string(b) != "payload\n" {
		t.Errorf("live content = %q", b)
	}

	// After soft-delete, ReadContent should follow the file into the morgue.
	moved, _ := s.MoveToMorgue(sc)
	b, err = s.ReadContent(moved)
	if err != nil {
		t.Fatalf("ReadContent(morgue): %v", err)
	}
	if string(b) != "payload\n" {
		t.Errorf("morgue content = %q", b)
	}
}

func TestPurgeAt(t *testing.T) {
	cfg := testConfig(t)
	s, _ := OpenWith(cfg)
	sc := seed(t, s, "ttl", "")

	// Live scratches have no purge deadline.
	if _, ok := s.PurgeAt(sc); ok {
		t.Error("live scratch should have no purge deadline")
	}

	moved, _ := s.MoveToMorgue(sc)
	at, ok := s.PurgeAt(moved)
	if !ok {
		t.Fatal("morgued scratch should have a purge deadline")
	}
	want := moved.DeletedAt.Add(cfg.Grace)
	if !at.Equal(want) {
		t.Errorf("PurgeAt = %v, want DeletedAt+grace %v", at, want)
	}
}

// TestMoveRollsBackOnIndexFailure isn't directly triggerable through the public
// API without breaking the index, so we at least assert the happy-path leaves
// no stray temp files in either directory.
func TestMoveLeavesNoTempFiles(t *testing.T) {
	cfg := testConfig(t)
	s, _ := OpenWith(cfg)
	sc := seed(t, s, "clean", "x")
	if _, err := s.MoveToMorgue(sc); err != nil {
		t.Fatalf("MoveToMorgue: %v", err)
	}
	for _, dir := range []string{cfg.ScratchesPath(), cfg.MorguePath()} {
		entries, _ := os.ReadDir(dir)
		for _, e := range entries {
			if filepath.Ext(e.Name()) == ".tmp" {
				t.Errorf("stray temp file in %s: %s", dir, e.Name())
			}
		}
	}
}
