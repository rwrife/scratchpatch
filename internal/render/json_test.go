package render

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"

	"github.com/rwrife/scratchpatch/internal/index"
)

func TestTableJSONOrderingAndStatus(t *testing.T) {
	now := time.Date(2026, 6, 30, 12, 0, 0, 0, time.UTC)
	scratches := []index.Scratch{
		// older, already expired
		mkScratch("old1", "stale", now.Add(-72*time.Hour), now.Add(-1*time.Hour), nil, "md", 10),
		// newest, expiring within the soon window (< 24h)
		mkScratch("new1", "soon", now.Add(-1*time.Hour), now.Add(6*time.Hour), []string{"a"}, "txt", 2048),
		// middle, comfortably fresh
		mkScratch("mid1", "fresh", now.Add(-12*time.Hour), now.Add(96*time.Hour), nil, "md", 100),
	}

	var buf bytes.Buffer
	if err := TableJSON(&buf, scratches, now); err != nil {
		t.Fatalf("TableJSON: %v", err)
	}

	var recs []ScratchJSON
	if err := json.Unmarshal(buf.Bytes(), &recs); err != nil {
		t.Fatalf("invalid JSON: %v (%q)", err, buf.String())
	}
	if len(recs) != 3 {
		t.Fatalf("expected 3 records, got %d", len(recs))
	}

	// Newest-created first: new1, mid1, old1.
	wantOrder := []string{"new1", "mid1", "old1"}
	for i, want := range wantOrder {
		if recs[i].ID != want {
			t.Errorf("record[%d].id = %q, want %q", i, recs[i].ID, want)
		}
	}

	// Status buckets mirror the table's classification.
	wantStatus := map[string]string{"new1": "soon", "mid1": "fresh", "old1": "expired"}
	for _, r := range recs {
		if got := wantStatus[r.ID]; r.Status != got {
			t.Errorf("%s status = %q, want %q", r.ID, r.Status, got)
		}
	}

	// The expired record reports a negative expiresInSeconds; the soon one is
	// positive and under a day.
	for _, r := range recs {
		switch r.ID {
		case "old1":
			if r.ExpiresInSeconds >= 0 {
				t.Errorf("expired scratch should have negative expiresInSeconds, got %d", r.ExpiresInSeconds)
			}
			if r.ExpiresHuman != "expired" {
				t.Errorf("expired scratch expiresHuman = %q, want \"expired\"", r.ExpiresHuman)
			}
		case "new1":
			if r.ExpiresInSeconds <= 0 || r.ExpiresInSeconds > int64((24*time.Hour)/time.Second) {
				t.Errorf("soon scratch expiresInSeconds = %d, want (0, 86400]", r.ExpiresInSeconds)
			}
		}
	}

	// nil tags serialize as [] (not null) so scripts get a stable shape.
	for _, r := range recs {
		if r.Tags == nil {
			t.Errorf("%s tags should be a non-nil empty slice", r.ID)
		}
	}
}

func TestTableJSONEmptyIsEmptyArray(t *testing.T) {
	var buf bytes.Buffer
	if err := TableJSON(&buf, nil, time.Now()); err != nil {
		t.Fatalf("TableJSON: %v", err)
	}
	var recs []ScratchJSON
	if err := json.Unmarshal(buf.Bytes(), &recs); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(recs) != 0 {
		t.Errorf("expected empty array, got %d records", len(recs))
	}
}

func TestMorgueTableJSONPurgeFields(t *testing.T) {
	now := time.Date(2026, 6, 30, 12, 0, 0, 0, time.UTC)

	delRecent := now.Add(-1 * time.Hour)
	delOld := now.Add(-100 * time.Hour)
	scRecent := mkScratch("rec1", "recent", now.Add(-2*time.Hour), time.Time{}, nil, "md", 5)
	scRecent.DeletedAt = &delRecent
	scOld := mkScratch("old1", "ancient", now.Add(-200*time.Hour), time.Time{}, nil, "md", 5)
	scOld.DeletedAt = &delOld

	rows := []MorgueRow{
		// recently deleted, still within grace (purge in the future)
		{Scratch: scRecent, PurgeAt: now.Add(47 * time.Hour)},
		// long-dead, past grace (already purgeable)
		{Scratch: scOld, PurgeAt: now.Add(-3 * time.Hour)},
	}

	var buf bytes.Buffer
	if err := MorgueTableJSON(&buf, rows, now); err != nil {
		t.Fatalf("MorgueTableJSON: %v", err)
	}

	var recs []MorgueJSON
	if err := json.Unmarshal(buf.Bytes(), &recs); err != nil {
		t.Fatalf("invalid JSON: %v (%q)", err, buf.String())
	}
	if len(recs) != 2 {
		t.Fatalf("expected 2 records, got %d", len(recs))
	}

	// Most-recently-deleted first: rec1 before old1.
	if recs[0].ID != "rec1" || recs[1].ID != "old1" {
		t.Errorf("morgue order = [%s, %s], want [rec1, old1]", recs[0].ID, recs[1].ID)
	}

	if recs[0].Purgeable {
		t.Errorf("rec1 is within grace and must not be purgeable")
	}
	if recs[0].PurgeInSeconds <= 0 {
		t.Errorf("rec1 purgeInSeconds = %d, want positive", recs[0].PurgeInSeconds)
	}
	if !recs[1].Purgeable {
		t.Errorf("old1 is past grace and must be purgeable")
	}
	if recs[1].PurgeHuman != "now" {
		t.Errorf("old1 purgeHuman = %q, want \"now\"", recs[1].PurgeHuman)
	}
}
