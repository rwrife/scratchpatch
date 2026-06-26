package index

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func sampleScratch(id string) Scratch {
	created := time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC)
	ttl := 7 * 24 * time.Hour
	return Scratch{
		ID:        id,
		Name:      "notes-" + id,
		CreatedAt: created,
		TTL:       Duration(ttl),
		ExpiresAt: created.Add(ttl),
		Tags:      []string{"scratch", "test"},
		Ext:       "md",
		OriginCwd: "/home/dev/project",
		Size:      42,
	}
}

func TestMissingIndexBootstraps(t *testing.T) {
	path := filepath.Join(t.TempDir(), "index.json")
	s := OpenJSON(path)

	// Reading a non-existent index must yield an empty list, not an error,
	// and must not create the file as a side effect of reading.
	got, err := s.List()
	if err != nil {
		t.Fatalf("List on missing index: unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("List on missing index: want empty, got %d records", len(got))
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("List must not create the index file; stat err = %v", err)
	}

	// Get on a missing index returns ErrNotFound (not a read error).
	if _, err := s.Get("nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get on missing index: want ErrNotFound, got %v", err)
	}
}

func TestIndexRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "index.json")
	s := OpenJSON(path)

	want := sampleScratch("abc123")
	if err := s.Put(want); err != nil {
		t.Fatalf("Put: %v", err)
	}

	// The file must now exist (Put creates it).
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("index file should exist after Put: %v", err)
	}

	// Read it back through a *fresh* store to prove persistence, not memory.
	got, err := OpenJSON(path).Get("abc123")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("round-trip mismatch:\n got=%+v\nwant=%+v", got, want)
	}

	// TTL must survive as a human-readable string, not a raw integer.
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read index file: %v", err)
	}
	if want, got := `"168h0m0s"`, string(raw); !contains(got, want) {
		t.Fatalf("ttl should serialize as %s; file was:\n%s", want, got)
	}
}

func TestPutReplacesAndList(t *testing.T) {
	path := filepath.Join(t.TempDir(), "index.json")
	s := OpenJSON(path)

	a := sampleScratch("a")
	a.CreatedAt = time.Date(2026, 6, 20, 0, 0, 0, 0, time.UTC)
	b := sampleScratch("b")
	b.CreatedAt = time.Date(2026, 6, 24, 0, 0, 0, 0, time.UTC)

	for _, sc := range []Scratch{a, b} {
		if err := s.Put(sc); err != nil {
			t.Fatalf("Put(%s): %v", sc.ID, err)
		}
	}

	// Replace a's name; count must stay 2.
	a.Name = "renamed"
	if err := s.Put(a); err != nil {
		t.Fatalf("Put replace: %v", err)
	}

	list, err := s.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("want 2 records after replace, got %d", len(list))
	}
	// Newest-first: b (Jun 24) before a (Jun 20).
	if list[0].ID != "b" || list[1].ID != "a" {
		t.Fatalf("want order [b a], got [%s %s]", list[0].ID, list[1].ID)
	}
	if list[1].Name != "renamed" {
		t.Fatalf("replace didn't take: name=%q", list[1].Name)
	}
}

func TestDelete(t *testing.T) {
	path := filepath.Join(t.TempDir(), "index.json")
	s := OpenJSON(path)

	if err := s.Put(sampleScratch("x")); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := s.Delete("x"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := s.Get("x"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("after Delete, Get should be ErrNotFound, got %v", err)
	}
	if err := s.Delete("x"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleting missing id should be ErrNotFound, got %v", err)
	}
}

func TestPutRejectsEmptyID(t *testing.T) {
	s := OpenJSON(filepath.Join(t.TempDir(), "index.json"))
	if err := s.Put(Scratch{}); err == nil {
		t.Fatal("Put with empty ID should error")
	}
}

func TestAtomicWriteLeavesNoTempFiles(t *testing.T) {
	dir := t.TempDir()
	s := OpenJSON(filepath.Join(dir, "index.json"))
	if err := s.Put(sampleScratch("t1")); err != nil {
		t.Fatalf("Put: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range entries {
		if e.Name() != "index.json" {
			t.Fatalf("unexpected leftover file after atomic write: %s", e.Name())
		}
	}
}

func TestCorruptIndexErrors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "index.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatalf("seed corrupt file: %v", err)
	}
	if _, err := OpenJSON(path).List(); err == nil {
		t.Fatal("List on corrupt index should error")
	}
}

func TestEmptyFileTreatedAsEmptyIndex(t *testing.T) {
	path := filepath.Join(t.TempDir(), "index.json")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatalf("seed empty file: %v", err)
	}
	got, err := OpenJSON(path).List()
	if err != nil {
		t.Fatalf("List on empty file: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("empty file should be empty index, got %d", len(got))
	}
}

func TestDurationJSON(t *testing.T) {
	// String round-trip.
	d := Duration(90 * time.Minute)
	b, err := json.Marshal(d)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(b) != `"1h30m0s"` {
		t.Fatalf("marshal got %s", b)
	}
	var back Duration
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatalf("unmarshal string: %v", err)
	}
	if back.Duration() != 90*time.Minute {
		t.Fatalf("round-trip got %s", back)
	}
	// Raw-nanosecond fallback.
	var fromInt Duration
	if err := json.Unmarshal([]byte("5400000000000"), &fromInt); err != nil {
		t.Fatalf("unmarshal int: %v", err)
	}
	if fromInt.Duration() != 90*time.Minute {
		t.Fatalf("int fallback got %s", fromInt)
	}
}

func contains(haystack, needle string) bool {
	return len(needle) == 0 ||
		(len(haystack) >= len(needle) && indexOf(haystack, needle) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
