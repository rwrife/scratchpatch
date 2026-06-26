package store

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rwrife/scratchpatch/internal/config"
	"github.com/rwrife/scratchpatch/internal/index"
)

func testConfig(t *testing.T) config.Config {
	t.Helper()
	return config.Config{
		Home:       t.TempDir(),
		DefaultTTL: 7 * 24 * time.Hour,
		DefaultExt: "md",
		Grace:      3 * 24 * time.Hour,
	}
}

func TestOpenCreatesLayout(t *testing.T) {
	cfg := testConfig(t)
	s, err := OpenWith(cfg)
	if err != nil {
		t.Fatalf("OpenWith: %v", err)
	}

	for _, dir := range []string{cfg.Home, cfg.ScratchesPath(), cfg.MorguePath()} {
		info, err := os.Stat(dir)
		if err != nil {
			t.Fatalf("expected dir %s: %v", dir, err)
		}
		if !info.IsDir() {
			t.Fatalf("%s is not a directory", dir)
		}
	}

	if s.Config().Home != cfg.Home {
		t.Errorf("Config().Home = %q, want %q", s.Config().Home, cfg.Home)
	}
	if s.Index() == nil {
		t.Error("Index() should not be nil")
	}
}

func TestOpenIsIdempotent(t *testing.T) {
	cfg := testConfig(t)
	if _, err := OpenWith(cfg); err != nil {
		t.Fatalf("first OpenWith: %v", err)
	}
	// Second call must not error even though dirs already exist.
	if _, err := OpenWith(cfg); err != nil {
		t.Fatalf("second OpenWith: %v", err)
	}
}

func TestStoreIndexIntegration(t *testing.T) {
	cfg := testConfig(t)
	s, err := OpenWith(cfg)
	if err != nil {
		t.Fatalf("OpenWith: %v", err)
	}

	rec := index.Scratch{
		ID:        "id-1",
		Name:      "thing",
		CreatedAt: time.Now().UTC().Truncate(time.Second),
		TTL:       index.Duration(cfg.DefaultTTL),
		Ext:       cfg.DefaultExt,
		OriginCwd: "/somewhere",
	}
	rec.ExpiresAt = rec.CreatedAt.Add(cfg.DefaultTTL)

	if err := s.Index().Put(rec); err != nil {
		t.Fatalf("Put via store index: %v", err)
	}

	// Index file should live at the configured path inside the store root.
	if _, err := os.Stat(cfg.IndexPath()); err != nil {
		t.Fatalf("index file missing at %s: %v", cfg.IndexPath(), err)
	}

	// Re-open the store (fresh handle) and confirm the record persisted.
	s2, err := OpenWith(cfg)
	if err != nil {
		t.Fatalf("re-open: %v", err)
	}
	got, err := s2.Index().Get("id-1")
	if err != nil {
		t.Fatalf("Get after re-open: %v", err)
	}
	if got.Name != "thing" {
		t.Fatalf("persisted name = %q, want thing", got.Name)
	}
}

func TestDirsArePrivate(t *testing.T) {
	cfg := testConfig(t)
	if _, err := OpenWith(cfg); err != nil {
		t.Fatalf("OpenWith: %v", err)
	}
	info, err := os.Stat(cfg.ScratchesPath())
	if err != nil {
		t.Fatalf("stat scratches: %v", err)
	}
	// On Unix the perms should be 0700; skip the exact bit check on systems
	// where it doesn't apply cleanly.
	if perm := info.Mode().Perm(); perm&0o077 != 0 {
		t.Errorf("scratches dir is group/other-accessible: %v at %s",
			perm, filepath.Base(cfg.ScratchesPath()))
	}
}
