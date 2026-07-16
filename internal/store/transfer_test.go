package store

import (
	"bytes"
	"os"
	"testing"
	"time"
)

// seedContent creates a live scratch and writes real content bytes so
// round-trips have something to compare.
func seedContent(t *testing.T, s *Store, name, body string) (id string, path string) {
	t.Helper()
	sc := seed(t, s, name, body)
	return sc.ID, s.ContentPath(sc)
}

func TestExportImportRoundTrip(t *testing.T) {
	src, _ := OpenWith(testConfig(t))
	idA, _ := seedContent(t, src, "alpha", "hello alpha\n")
	idB, _ := seedContent(t, src, "beta", "hello beta\n")

	var buf bytes.Buffer
	if err := src.Export(&buf, ExportOptions{}); err != nil {
		t.Fatalf("Export: %v", err)
	}

	dst, _ := OpenWith(testConfig(t))
	res, err := dst.Import(&buf, ImportMerge)
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if len(res.Added) != 2 {
		t.Fatalf("Added = %v, want 2", res.Added)
	}

	for id, want := range map[string]string{idA: "hello alpha\n", idB: "hello beta\n"} {
		sc, err := dst.Index().Get(id)
		if err != nil {
			t.Fatalf("Get(%s): %v", id, err)
		}
		got, err := os.ReadFile(dst.LivePath(sc))
		if err != nil {
			t.Fatalf("read content for %s: %v", id, err)
		}
		if string(got) != want {
			t.Errorf("content for %s = %q, want %q", id, got, want)
		}
	}
}

func TestExportImportMetadataPreserved(t *testing.T) {
	src, _ := OpenWith(testConfig(t))
	sc := seed(t, src, "meta", "body\n")
	sc.Tags = []string{"keep", "me"}
	sc.CreatedAt = time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	if err := src.Index().Put(sc); err != nil {
		t.Fatalf("Put: %v", err)
	}

	var buf bytes.Buffer
	if err := src.Export(&buf, ExportOptions{}); err != nil {
		t.Fatalf("Export: %v", err)
	}

	dst, _ := OpenWith(testConfig(t))
	if _, err := dst.Import(&buf, ImportMerge); err != nil {
		t.Fatalf("Import: %v", err)
	}
	got, err := dst.Index().Get(sc.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(got.Tags) != 2 || got.Tags[0] != "keep" || got.Tags[1] != "me" {
		t.Errorf("tags = %v, want [keep me]", got.Tags)
	}
	if !got.CreatedAt.Equal(sc.CreatedAt) {
		t.Errorf("CreatedAt = %v, want %v", got.CreatedAt, sc.CreatedAt)
	}
}

func TestImportMergeSkipsCollisions(t *testing.T) {
	src, _ := OpenWith(testConfig(t))
	idA, _ := seedContent(t, src, "alpha", "source alpha\n")

	var buf bytes.Buffer
	if err := src.Export(&buf, ExportOptions{}); err != nil {
		t.Fatalf("Export: %v", err)
	}

	// Destination already holds the same id with different content.
	dst, _ := OpenWith(testConfig(t))
	existing := seed(t, dst, "existing", "DO NOT CLOBBER\n")
	// Force the destination scratch to share the source id.
	if err := dst.Index().Delete(existing.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	_ = os.Remove(dst.ContentPath(existing))
	existing.ID = idA
	if err := dst.Index().Put(existing); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := os.WriteFile(dst.ContentPath(existing), []byte("DO NOT CLOBBER\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	res, err := dst.Import(&buf, ImportMerge)
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if len(res.Added) != 0 {
		t.Errorf("Added = %v, want none", res.Added)
	}
	if len(res.Skipped) != 1 || res.Skipped[0] != idA {
		t.Errorf("Skipped = %v, want [%s]", res.Skipped, idA)
	}

	got, _ := dst.Index().Get(idA)
	content, _ := os.ReadFile(dst.LivePath(got))
	if string(content) != "DO NOT CLOBBER\n" {
		t.Errorf("merge clobbered existing content: %q", content)
	}
}

func TestExportEmptyStore(t *testing.T) {
	src, _ := OpenWith(testConfig(t))

	var buf bytes.Buffer
	if err := src.Export(&buf, ExportOptions{}); err != nil {
		t.Fatalf("Export empty: %v", err)
	}
	if buf.Len() == 0 {
		t.Fatal("empty-store export produced no bytes")
	}

	dst, _ := OpenWith(testConfig(t))
	res, err := dst.Import(&buf, ImportMerge)
	if err != nil {
		t.Fatalf("Import empty: %v", err)
	}
	if len(res.Added) != 0 || len(res.Skipped) != 0 {
		t.Errorf("empty import did something: %+v", res)
	}
}

func TestExportIncludeMorgue(t *testing.T) {
	src, _ := OpenWith(testConfig(t))
	live, _ := seedContent(t, src, "live", "alive\n")
	dead := seedMorgued(t, src, "dead", time.Now().Add(-time.Hour), "buried\n")

	// Without --include-morgue: only the live scratch travels.
	var noMorgue bytes.Buffer
	if err := src.Export(&noMorgue, ExportOptions{}); err != nil {
		t.Fatalf("Export: %v", err)
	}
	dst1, _ := OpenWith(testConfig(t))
	res1, _ := dst1.Import(&noMorgue, ImportMerge)
	if len(res1.Added) != 1 || res1.Added[0] != live {
		t.Errorf("live-only Added = %v, want [%s]", res1.Added, live)
	}

	// With --include-morgue: both travel, and the dead one lands in the morgue.
	var withMorgue bytes.Buffer
	if err := src.Export(&withMorgue, ExportOptions{IncludeMorgue: true}); err != nil {
		t.Fatalf("Export: %v", err)
	}
	dst2, _ := OpenWith(testConfig(t))
	res2, _ := dst2.Import(&withMorgue, ImportMerge)
	if len(res2.Added) != 2 {
		t.Fatalf("with-morgue Added = %v, want 2", res2.Added)
	}
	got, err := dst2.Index().Get(dead.ID)
	if err != nil {
		t.Fatalf("Get dead: %v", err)
	}
	if !got.Morgued() {
		t.Error("imported morgue scratch should still be morgued")
	}
	if _, err := os.Stat(dst2.morguePath(got)); err != nil {
		t.Errorf("morgue content missing: %v", err)
	}
}

func TestImportReplaceBacksUpAndReplaces(t *testing.T) {
	src, _ := OpenWith(testConfig(t))
	newID, _ := seedContent(t, src, "incoming", "fresh\n")

	var buf bytes.Buffer
	if err := src.Export(&buf, ExportOptions{}); err != nil {
		t.Fatalf("Export: %v", err)
	}

	dst, _ := OpenWith(testConfig(t))
	oldID, _ := seedContent(t, dst, "stale", "old\n")

	res, err := dst.Import(&buf, ImportReplace)
	if err != nil {
		t.Fatalf("Import replace: %v", err)
	}
	if res.BackupPath == "" {
		t.Fatal("replace should record a backup path")
	}
	if _, err := os.Stat(res.BackupPath); err != nil {
		t.Errorf("backup file missing: %v", err)
	}
	// Old scratch is gone, new one present.
	if _, err := dst.Index().Get(oldID); err == nil {
		t.Errorf("old scratch %s should be gone after replace", oldID)
	}
	if _, err := dst.Index().Get(newID); err != nil {
		t.Errorf("new scratch %s should be present after replace: %v", newID, err)
	}
}

func TestImportRejectsNonExport(t *testing.T) {
	dst, _ := OpenWith(testConfig(t))
	// Random gzip'd bytes that aren't a scratchpatch export.
	var buf bytes.Buffer
	// A valid-but-empty gzip stream via Export of empty store would pass; use
	// plainly bogus bytes to exercise the gzip-open error path instead.
	buf.WriteString("not a gzip stream at all")
	if _, err := dst.Import(&buf, ImportMerge); err == nil {
		t.Error("importing garbage should error")
	}
}
