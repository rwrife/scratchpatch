package store

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/rwrife/scratchpatch/internal/index"
)

func TestPromoteMovesContentOutAndForgetsScratch(t *testing.T) {
	s, _ := OpenWith(testConfig(t))
	sc := seed(t, s, "keeper", "worth keeping\n")

	livePath := s.ContentPath(sc)
	dest := filepath.Join(t.TempDir(), "keeper.md")

	if err := s.Promote(sc, dest, false); err != nil {
		t.Fatalf("Promote: %v", err)
	}

	// Content left the store and landed at the destination unchanged.
	if _, err := os.Stat(livePath); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("store content should be gone after promote, stat err = %v", err)
	}
	body, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("read promoted file: %v", err)
	}
	if string(body) != "worth keeping\n" {
		t.Errorf("promoted content = %q, want %q", body, "worth keeping\n")
	}

	// The index no longer tracks it — the repo owns it now.
	if _, err := s.Index().Get(sc.ID); !errors.Is(err, index.ErrNotFound) {
		t.Errorf("promoted scratch should be dropped from index, got err = %v", err)
	}
}

func TestPromoteFromMorgue(t *testing.T) {
	s, _ := OpenWith(testConfig(t))
	sc := seed(t, s, "second-chance", "rescued\n")
	morgued, err := s.MoveToMorgue(sc)
	if err != nil {
		t.Fatalf("MoveToMorgue: %v", err)
	}

	dest := filepath.Join(t.TempDir(), "rescued.md")
	if err := s.Promote(morgued, dest, false); err != nil {
		t.Fatalf("Promote from morgue: %v", err)
	}

	if _, err := os.Stat(s.morguePath(morgued)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("morgue content should be gone after promote, stat err = %v", err)
	}
	body, _ := os.ReadFile(dest)
	if string(body) != "rescued\n" {
		t.Errorf("promoted morgue content = %q, want %q", body, "rescued\n")
	}
	if _, err := s.Index().Get(morgued.ID); !errors.Is(err, index.ErrNotFound) {
		t.Errorf("promoted scratch should leave the index, got err = %v", err)
	}
}

func TestPromoteRefusesToOverwriteWithoutForce(t *testing.T) {
	s, _ := OpenWith(testConfig(t))
	sc := seed(t, s, "collide", "new body\n")

	dest := filepath.Join(t.TempDir(), "existing.md")
	if err := os.WriteFile(dest, []byte("do not clobber\n"), 0o600); err != nil {
		t.Fatalf("pre-write dest: %v", err)
	}

	err := s.Promote(sc, dest, false)
	if !errors.Is(err, ErrDestinationExists) {
		t.Fatalf("expected ErrDestinationExists, got %v", err)
	}

	// The guard must be non-destructive: destination untouched, scratch intact.
	body, _ := os.ReadFile(dest)
	if string(body) != "do not clobber\n" {
		t.Errorf("destination should be untouched, got %q", body)
	}
	if _, err := s.Index().Get(sc.ID); err != nil {
		t.Errorf("scratch should survive a refused promote, got %v", err)
	}
	if _, err := os.Stat(s.ContentPath(sc)); err != nil {
		t.Errorf("scratch content should survive a refused promote, got %v", err)
	}
}

func TestPromoteForceOverwrites(t *testing.T) {
	s, _ := OpenWith(testConfig(t))
	sc := seed(t, s, "winner", "fresh\n")

	dest := filepath.Join(t.TempDir(), "target.md")
	if err := os.WriteFile(dest, []byte("stale\n"), 0o600); err != nil {
		t.Fatalf("pre-write dest: %v", err)
	}

	if err := s.Promote(sc, dest, true); err != nil {
		t.Fatalf("Promote --force: %v", err)
	}
	body, _ := os.ReadFile(dest)
	if string(body) != "fresh\n" {
		t.Errorf("force promote should overwrite, got %q", body)
	}
}

func TestPromoteCreatesMissingDestDir(t *testing.T) {
	s, _ := OpenWith(testConfig(t))
	sc := seed(t, s, "nested", "deep\n")

	dest := filepath.Join(t.TempDir(), "a", "b", "c", "nested.md")
	if err := s.Promote(sc, dest, false); err != nil {
		t.Fatalf("Promote into nested dir: %v", err)
	}
	if _, err := os.Stat(dest); err != nil {
		t.Errorf("nested destination should exist, got %v", err)
	}
}
