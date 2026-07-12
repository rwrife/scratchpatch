package render

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"
)

func sampleDedupData() DedupData {
	created := time.Now().Add(-time.Hour)
	return DedupData{
		ScannedCount: 4,
		TotalWasted:  20,
		Clusters: []DedupClusterData{
			{
				Digest:      "abc123def456abc123def456",
				WastedBytes: 20,
				Members: []DedupMemberData{
					{ID: "old00001", Name: "original", Size: 20, CreatedAt: created, Canonical: true},
					{ID: "new00002", Name: "copy", Size: 20, CreatedAt: created.Add(time.Minute)},
				},
			},
		},
	}
}

// TestDedupReportCleanStore: a unique store gets a reassuring, cluster-free
// headline and the read-only footer.
func TestDedupReportCleanStore(t *testing.T) {
	var buf bytes.Buffer
	if err := DedupReport(&buf, DedupData{ScannedCount: 5}, false); err != nil {
		t.Fatalf("DedupReport: %v", err)
	}
	out := buf.String()
	if !bytes.Contains(buf.Bytes(), []byte("no duplicates")) {
		t.Errorf("clean report should say no duplicates; got %q", out)
	}
}

// TestDedupReportListsClusters: a store with duplicates names the canonical
// keep-copy and the redundant extras, and (read-only) points at --collapse.
func TestDedupReportListsClusters(t *testing.T) {
	var buf bytes.Buffer
	if err := DedupReport(&buf, sampleDedupData(), false); err != nil {
		t.Fatalf("DedupReport: %v", err)
	}
	out := buf.String()
	for _, want := range []string{"old00001", "new00002", "canonical", "redundant", "--collapse"} {
		if !bytes.Contains(buf.Bytes(), []byte(want)) {
			t.Errorf("report missing %q; got:\n%s", want, out)
		}
	}
}

// TestDedupReportCollapseConfirmation: after a collapse the report confirms
// what moved to the morgue instead of the read-only footer.
func TestDedupReportCollapseConfirmation(t *testing.T) {
	data := sampleDedupData()
	data.Collapsed = &DedupCollapsedData{MovedIDs: []string{"new00002"}, ReclaimedBytes: 20}

	var buf bytes.Buffer
	if err := DedupReport(&buf, data, false); err != nil {
		t.Fatalf("DedupReport: %v", err)
	}
	if !bytes.Contains(buf.Bytes(), []byte("collapsed")) || !bytes.Contains(buf.Bytes(), []byte("morgue")) {
		t.Errorf("collapse report should confirm the move; got %q", buf.String())
	}
	if bytes.Contains(buf.Bytes(), []byte("--collapse to send")) {
		t.Errorf("collapse report should not still suggest --collapse; got %q", buf.String())
	}
}

// TestDedupReportJSONShape: --json is colorless, personality-free, and carries
// the stable contract (clean flag, non-nil arrays, full digest, collapsed null).
func TestDedupReportJSONShape(t *testing.T) {
	var buf bytes.Buffer
	if err := DedupReportJSON(&buf, sampleDedupData()); err != nil {
		t.Fatalf("DedupReportJSON: %v", err)
	}
	if bytes.Contains(buf.Bytes(), []byte("\x1b[")) {
		t.Errorf("dedup --json must be colorless; got %q", buf.String())
	}
	for _, flavor := range []string{"canonical — the original", "haunting", "reclaimable"} {
		if bytes.Contains(buf.Bytes(), []byte(flavor)) {
			t.Errorf("dedup --json must be personality-free; found %q", flavor)
		}
	}

	var rec DedupJSON
	if err := json.Unmarshal(buf.Bytes(), &rec); err != nil {
		t.Fatalf("invalid JSON: %v (%q)", err, buf.String())
	}
	if rec.Clean {
		t.Errorf("store with a cluster must report clean=false")
	}
	if rec.ClusterCount != 1 || rec.ScannedCount != 4 {
		t.Errorf("clusterCount=%d scannedCount=%d, want 1/4", rec.ClusterCount, rec.ScannedCount)
	}
	if rec.TotalWasted != 20 {
		t.Errorf("totalWasted=%d, want 20", rec.TotalWasted)
	}
	if len(rec.Clusters) != 1 || rec.Clusters[0].Count != 2 {
		t.Fatalf("clusters shape wrong: %+v", rec.Clusters)
	}
	if rec.Clusters[0].Digest != "abc123def456abc123def456" {
		t.Errorf("json should carry the full digest, got %q", rec.Clusters[0].Digest)
	}
	if !rec.Clusters[0].Members[0].Canonical || rec.Clusters[0].Members[1].Canonical {
		t.Errorf("member canonical flags wrong: %+v", rec.Clusters[0].Members)
	}
	if rec.Collapsed != nil {
		t.Errorf("collapsed should be null when no --collapse ran; got %+v", rec.Collapsed)
	}
}

// TestDedupReportJSONCleanArrays: a clean store still emits a non-null clusters
// array so scripts can iterate unconditionally.
func TestDedupReportJSONCleanArrays(t *testing.T) {
	var buf bytes.Buffer
	if err := DedupReportJSON(&buf, DedupData{ScannedCount: 2}); err != nil {
		t.Fatalf("DedupReportJSON: %v", err)
	}
	var rec DedupJSON
	if err := json.Unmarshal(buf.Bytes(), &rec); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if !rec.Clean {
		t.Errorf("empty-cluster store should report clean=true")
	}
	if rec.Clusters == nil || len(rec.Clusters) != 0 {
		t.Errorf("clusters should be empty non-nil array; got %#v", rec.Clusters)
	}
}

// TestDedupReportJSONCollapsed: a collapse run surfaces the collapsed object
// with the moved ids and reclaimed bytes.
func TestDedupReportJSONCollapsed(t *testing.T) {
	data := sampleDedupData()
	data.Collapsed = &DedupCollapsedData{MovedIDs: []string{"new00002"}, ReclaimedBytes: 20}

	var buf bytes.Buffer
	if err := DedupReportJSON(&buf, data); err != nil {
		t.Fatalf("DedupReportJSON: %v", err)
	}
	var rec DedupJSON
	if err := json.Unmarshal(buf.Bytes(), &rec); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if rec.Collapsed == nil {
		t.Fatalf("collapsed should be populated after a collapse run")
	}
	if len(rec.Collapsed.MovedIDs) != 1 || rec.Collapsed.MovedIDs[0] != "new00002" {
		t.Errorf("movedIds = %v, want [new00002]", rec.Collapsed.MovedIDs)
	}
	if rec.Collapsed.ReclaimedBytes != 20 {
		t.Errorf("reclaimedBytes = %d, want 20", rec.Collapsed.ReclaimedBytes)
	}
}
