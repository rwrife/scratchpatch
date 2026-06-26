package store

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestCreateWritesFileAndIndexesMetadata(t *testing.T) {
	cfg := testConfig(t)
	s, err := OpenWith(cfg)
	if err != nil {
		t.Fatalf("OpenWith: %v", err)
	}

	sc, path, err := s.Create(CreateOptions{
		Name: "notes",
		Ext:  "txt",
		TTL:  2 * time.Hour,
		Tags: []string{"a", "b"},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Content file exists, is empty, and lives under scratches/ as id.ext.
	wantPath := filepath.Join(cfg.ScratchesPath(), sc.ID+".txt")
	if path != wantPath {
		t.Errorf("content path = %q, want %q", path, wantPath)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat content file: %v", err)
	}
	if info.Size() != 0 {
		t.Errorf("new scratch file should be empty, got %d bytes", info.Size())
	}
	if perm := info.Mode().Perm(); perm&0o077 != 0 {
		t.Errorf("scratch file is group/other-accessible: %v", perm)
	}

	// Metadata round-trips through the index.
	got, err := s.Index().Get(sc.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Name != "notes" || got.Ext != "txt" {
		t.Errorf("metadata = %+v, want name=notes ext=txt", got)
	}
	if got.TTL.Duration() != 2*time.Hour {
		t.Errorf("ttl = %v, want 2h", got.TTL.Duration())
	}
	if !got.ExpiresAt.Equal(got.CreatedAt.Add(2 * time.Hour)) {
		t.Errorf("expiresAt %v != createdAt+ttl %v", got.ExpiresAt, got.CreatedAt.Add(2*time.Hour))
	}
	if !reflect.DeepEqual(got.Tags, []string{"a", "b"}) {
		t.Errorf("tags = %v, want [a b]", got.Tags)
	}
	if got.OriginCwd == "" {
		t.Error("originCwd should default to the working directory")
	}
}

func TestCreateAppliesDefaults(t *testing.T) {
	cfg := testConfig(t)
	s, err := OpenWith(cfg)
	if err != nil {
		t.Fatalf("OpenWith: %v", err)
	}

	sc, _, err := s.Create(CreateOptions{Name: "d"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if sc.Ext != cfg.DefaultExt {
		t.Errorf("ext = %q, want default %q", sc.Ext, cfg.DefaultExt)
	}
	if sc.TTL.Duration() != cfg.DefaultTTL {
		t.Errorf("ttl = %v, want default %v", sc.TTL.Duration(), cfg.DefaultTTL)
	}
}

func TestCreateStripsLeadingDotOnExt(t *testing.T) {
	cfg := testConfig(t)
	s, _ := OpenWith(cfg)
	sc, path, err := s.Create(CreateOptions{Ext: ".json"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if sc.Ext != "json" {
		t.Errorf("ext = %q, want json (no leading dot)", sc.Ext)
	}
	if filepath.Ext(path) != ".json" {
		t.Errorf("path ext = %q, want .json", filepath.Ext(path))
	}
}

func TestCreateGeneratesUniqueIDs(t *testing.T) {
	cfg := testConfig(t)
	s, _ := OpenWith(cfg)
	seen := map[string]bool{}
	for i := 0; i < 25; i++ {
		sc, _, err := s.Create(CreateOptions{})
		if err != nil {
			t.Fatalf("Create #%d: %v", i, err)
		}
		if seen[sc.ID] {
			t.Fatalf("duplicate id generated: %s", sc.ID)
		}
		seen[sc.ID] = true
	}
}

func TestTouchRefreshesSize(t *testing.T) {
	cfg := testConfig(t)
	s, _ := OpenWith(cfg)
	sc, path, err := s.Create(CreateOptions{Name: "sz"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if sc.Size != 0 {
		t.Fatalf("fresh scratch size = %d, want 0", sc.Size)
	}

	body := []byte("hello world\n")
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatalf("write content: %v", err)
	}

	touched, err := s.Touch(sc.ID)
	if err != nil {
		t.Fatalf("Touch: %v", err)
	}
	if touched.Size != int64(len(body)) {
		t.Errorf("touched size = %d, want %d", touched.Size, len(body))
	}

	// Persisted, not just returned.
	got, _ := s.Index().Get(sc.ID)
	if got.Size != int64(len(body)) {
		t.Errorf("persisted size = %d, want %d", got.Size, len(body))
	}
}

func TestNormalizeTagsDedupesAndTrims(t *testing.T) {
	got := normalizeTags([]string{" a ", "a", "", "b", "  "})
	want := []string{"a", "b"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("normalizeTags = %v, want %v", got, want)
	}
	if normalizeTags(nil) != nil {
		t.Error("normalizeTags(nil) should be nil")
	}
	if normalizeTags([]string{"", "   "}) != nil {
		t.Error("normalizeTags of all-empty should be nil")
	}
}
