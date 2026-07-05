package render

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/rwrife/scratchpatch/internal/index"
)

func TestScanReportCleanIsReassuring(t *testing.T) {
	var buf bytes.Buffer
	d := ScanReportData{ID: "abc123", Name: "notes"}
	if err := ScanReport(&buf, d, false); err != nil {
		t.Fatalf("ScanReport: %v", err)
	}
	got := buf.String()
	if !strings.Contains(got, "clean bill of health") {
		t.Errorf("clean scan should say so; got %q", got)
	}
	if strings.Contains(got, "🔑") {
		t.Errorf("clean report needs no marker; got %q", got)
	}
}

func TestScanReportListsMaskedFindings(t *testing.T) {
	var buf bytes.Buffer
	d := ScanReportData{
		ID:   "def456",
		Name: "leaky",
		Findings: []ScanFinding{
			{Kind: "aws-access-key", Line: 2, Rule: "AWS access key id", Masked: "AKI…PLE"},
			{Kind: "assignment", Line: 3, Rule: "secret-looking assignment (TOKEN)", Masked: "EXA…nJx"},
		},
	}
	if err := ScanReport(&buf, d, false); err != nil {
		t.Fatalf("ScanReport: %v", err)
	}
	got := buf.String()
	for _, want := range []string{"tripwire", "L2", "L3", "AKI…PLE", "EXA…nJx", "--allow-secrets"} {
		if !strings.Contains(got, want) {
			t.Errorf("scan report missing %q; got %q", want, got)
		}
	}
}

func TestScanReportJSONIsPureData(t *testing.T) {
	var buf bytes.Buffer
	d := ScanReportData{
		ID:       "ghi789",
		Name:     "leaky",
		Findings: []ScanFinding{{Kind: "high-entropy", Line: 1, Rule: "high-entropy token", Masked: "9f8…iE0"}},
	}
	if err := ScanReportJSON(&buf, d); err != nil {
		t.Fatalf("ScanReportJSON: %v", err)
	}
	got := buf.String()
	for _, want := range []string{`"id": "ghi789"`, `"tripped": true`, `"findings"`, `"masked": "9f8…iE0"`} {
		if !strings.Contains(got, want) {
			t.Errorf("scan JSON missing %q; got %s", want, got)
		}
	}
	if strings.Contains(got, "tripwire") {
		t.Errorf("scan JSON should carry no personality; got %s", got)
	}
}

func TestScanReportJSONEmptyFindingsIsArrayNotNull(t *testing.T) {
	var buf bytes.Buffer
	d := ScanReportData{ID: "x", Name: ""}
	if err := ScanReportJSON(&buf, d); err != nil {
		t.Fatalf("ScanReportJSON: %v", err)
	}
	got := buf.String()
	if !strings.Contains(got, `"findings": []`) {
		t.Errorf("empty findings should serialize as [], not null; got %s", got)
	}
	if !strings.Contains(got, `"tripped": false`) {
		t.Errorf("clean scan should report tripped=false; got %s", got)
	}
}

func TestTableMarkedFlagsNameWithKey(t *testing.T) {
	now := time.Now()
	s := index.Scratch{
		ID:        "aaa111",
		Name:      "secrets",
		CreatedAt: now.Add(-time.Hour),
		ExpiresAt: now.Add(48 * time.Hour),
	}
	var marked, plain bytes.Buffer
	if err := TableMarked(&marked, []index.Scratch{s}, map[string]bool{"aaa111": true}, now, false); err != nil {
		t.Fatalf("TableMarked: %v", err)
	}
	if err := TableMarked(&plain, []index.Scratch{s}, nil, now, false); err != nil {
		t.Fatalf("TableMarked(nil markers): %v", err)
	}
	if !strings.Contains(marked.String(), "🔑") {
		t.Errorf("marked table should show 🔑; got %q", marked.String())
	}
	if strings.Contains(plain.String(), "🔑") {
		t.Errorf("unmarked table should not show 🔑; got %q", plain.String())
	}
}
