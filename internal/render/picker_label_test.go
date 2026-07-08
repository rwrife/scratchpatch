package render

import (
	"strings"
	"testing"
	"time"

	"github.com/rwrife/scratchpatch/internal/index"
)

func TestPickerLabelShowsKeyFields(t *testing.T) {
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	sc := index.Scratch{
		ID:        "deadbeef",
		Name:      "todo-list",
		CreatedAt: now.Add(-3 * 24 * time.Hour), // 3d old
		ExpiresAt: now.Add(4 * 24 * time.Hour),  // expires in 4d
		Tags:      []string{"work", "q3"},
	}
	label := PickerLabel(sc, now)

	for _, want := range []string{"deadbeef", "todo-list", "3d", "in 4d", "work,q3"} {
		if !strings.Contains(label, want) {
			t.Errorf("picker label missing %q; got %q", want, label)
		}
	}
	// Labels are consumed as raw lines (and matched against) by the picker, so
	// they must not carry ANSI escapes.
	if strings.Contains(label, "\x1b[") {
		t.Errorf("picker label should be plain (no ANSI); got %q", label)
	}
}

func TestPickerLabelUnnamedAndUntagged(t *testing.T) {
	now := time.Now()
	sc := index.Scratch{ID: "abcd1234", CreatedAt: now, ExpiresAt: now.Add(time.Hour)}
	label := PickerLabel(sc, now)
	if !strings.Contains(label, "abcd1234") {
		t.Errorf("label should include the id; got %q", label)
	}
	// Name and tags fall back to a dash rather than blanks.
	if !strings.Contains(label, "-") {
		t.Errorf("unnamed/untagged scratch should show dashes; got %q", label)
	}
}
