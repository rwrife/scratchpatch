package render

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/rwrife/scratchpatch/internal/index"
)

// TestTableMarkedShowsPinGlyphOnTTY verifies a pinned scratch renders the 📌
// glyph in the colorized (non-plain) table.
func TestTableMarkedShowsPinGlyphOnTTY(t *testing.T) {
	now := time.Now()
	s := index.Scratch{
		ID:        "pin111",
		Name:      "keeper",
		CreatedAt: now.Add(-time.Hour),
		ExpiresAt: now.Add(48 * time.Hour),
		Pinned:    true,
	}
	var buf bytes.Buffer
	if err := TableMarked(&buf, []index.Scratch{s}, nil, now, true); err != nil {
		t.Fatalf("TableMarked: %v", err)
	}
	if !strings.Contains(buf.String(), "📌") {
		t.Errorf("pinned scratch should show 📌 on a TTY; got %q", buf.String())
	}
}

// TestPlainTableShowsPINToken verifies the pin degrades to the ASCII PIN token
// in plain (piped) output rather than the wide glyph.
func TestPlainTableShowsPINToken(t *testing.T) {
	now := time.Now()
	s := index.Scratch{
		ID:        "pin222",
		Name:      "keeper",
		CreatedAt: now.Add(-time.Hour),
		ExpiresAt: now.Add(48 * time.Hour),
		Pinned:    true,
	}
	var buf bytes.Buffer
	if err := TableMarked(&buf, []index.Scratch{s}, nil, now, false); err != nil {
		t.Fatalf("TableMarked: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "PIN") {
		t.Errorf("plain output should carry the PIN token; got %q", out)
	}
	if strings.Contains(out, "📌") {
		t.Errorf("plain output should not carry the 📌 glyph; got %q", out)
	}
}

// TestTableJSONCarriesPinned verifies the --json record surfaces the pin state.
func TestTableJSONCarriesPinned(t *testing.T) {
	now := time.Now()
	pinned := index.Scratch{ID: "aaa", CreatedAt: now, ExpiresAt: now.Add(time.Hour), Pinned: true}
	loose := index.Scratch{ID: "bbb", CreatedAt: now, ExpiresAt: now.Add(time.Hour)}

	var buf bytes.Buffer
	if err := TableJSON(&buf, []index.Scratch{pinned, loose}, now); err != nil {
		t.Fatalf("TableJSON: %v", err)
	}
	var recs []ScratchJSON
	if err := json.Unmarshal(buf.Bytes(), &recs); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	got := map[string]bool{}
	for _, r := range recs {
		got[r.ID] = r.Pinned
	}
	if !got["aaa"] {
		t.Error("pinned scratch should report pinned=true in JSON")
	}
	if got["bbb"] {
		t.Error("unpinned scratch should report pinned=false in JSON")
	}
}

// TestReapSummaryNotesPinnedSkips verifies the reap summary reports how many
// pinned scratches were spared.
func TestReapSummaryNotesPinnedSkips(t *testing.T) {
	var buf bytes.Buffer
	res := ReapResult{
		Swept:         []index.Scratch{{ID: "x", Name: "gone"}},
		PinnedSkipped: 2,
	}
	if err := ReapSummary(&buf, res, false); err != nil {
		t.Fatalf("ReapSummary: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "2 pinned scratches") || !strings.Contains(out, "pin") {
		t.Errorf("summary should note the pinned skip count; got %q", out)
	}
}

// TestReapSummaryEmptyNotesPinnedSkips verifies the "nothing to reap" line still
// mentions pinned skips when a pin was the only thing that stopped a sweep.
func TestReapSummaryEmptyNotesPinnedSkips(t *testing.T) {
	var buf bytes.Buffer
	if err := ReapSummary(&buf, ReapResult{PinnedSkipped: 1}, false); err != nil {
		t.Fatalf("ReapSummary: %v", err)
	}
	if !strings.Contains(buf.String(), "1 pinned scratch") {
		t.Errorf("empty summary should note the spared pin; got %q", buf.String())
	}
}
