package render

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestStatsReportEmptyStore(t *testing.T) {
	var b bytes.Buffer
	if err := StatsReport(&b, StatsData{}, false); err != nil {
		t.Fatalf("StatsReport: %v", err)
	}
	out := b.String()
	if !strings.Contains(out, "empty") {
		t.Errorf("empty store report should mention emptiness, got %q", out)
	}
	if strings.Contains(out, "\x1b[") {
		t.Errorf("color=false output should carry no escape codes: %q", out)
	}
}

func TestStatsReportPopulated(t *testing.T) {
	d := StatsData{
		LiveCount:      2,
		LiveBytes:      2048,
		MorgueCount:    1,
		MorgueBytes:    512,
		PurgeableCount: 1,
		TotalBytes:     2560,
		OldestID:       "deadbeef",
		OldestName:     "notes",
		OldestAge:      100 * time.Hour,
		Tags:           []StatsTag{{"work", 2}, {"todo", 1}},
		Grace:          72 * time.Hour,
	}
	var b bytes.Buffer
	if err := StatsReport(&b, d, false); err != nil {
		t.Fatalf("StatsReport: %v", err)
	}
	out := b.String()
	for _, want := range []string{"deadbeef", "notes", "oldest survivor", "work (2)", "todo (1)", "grace"} {
		if !strings.Contains(out, want) {
			t.Errorf("report missing %q; got:\n%s", want, out)
		}
	}
}

func TestStatsReportJSONShape(t *testing.T) {
	d := StatsData{
		LiveCount:   1,
		LiveBytes:   1024,
		MorgueCount: 0,
		TotalBytes:  1024,
		OldestID:    "abc123",
		OldestName:  "x",
		OldestAge:   2 * time.Hour,
		Tags:        []StatsTag{{"a", 1}},
		Grace:       72 * time.Hour,
	}
	var b bytes.Buffer
	if err := StatsReportJSON(&b, d); err != nil {
		t.Fatalf("StatsReportJSON: %v", err)
	}
	var got StatsJSON
	if err := json.Unmarshal(b.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, b.String())
	}
	if got.LiveBytesHuman == "" || got.TotalBytesHuman == "" {
		t.Error("human size companions should be populated")
	}
	if got.GraceSeconds != int64((72 * time.Hour).Seconds()) {
		t.Errorf("GraceSeconds = %d", got.GraceSeconds)
	}
	if got.Oldest == nil || got.Oldest.ID != "abc123" {
		t.Fatalf("Oldest should be present with id abc123, got %+v", got.Oldest)
	}
	if len(got.Tags) != 1 || got.Tags[0].Tag != "a" {
		t.Errorf("Tags = %+v", got.Tags)
	}
}

func TestStatsReportJSONNullOldestWhenEmpty(t *testing.T) {
	var b bytes.Buffer
	if err := StatsReportJSON(&b, StatsData{Grace: time.Hour}); err != nil {
		t.Fatalf("StatsReportJSON: %v", err)
	}
	// Oldest must serialize as null (not an empty object) when there are no
	// live scratches, and tags must be [] not null.
	s := b.String()
	if !strings.Contains(s, "\"oldest\": null") {
		t.Errorf("oldest should be null on empty store: %s", s)
	}
	if !strings.Contains(s, "\"tags\": []") {
		t.Errorf("tags should be [] on empty store: %s", s)
	}
}
