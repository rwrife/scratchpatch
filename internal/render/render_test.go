package render

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/rwrife/scratchpatch/internal/index"
)

// forceColorProfile pins lipgloss to a truecolor profile for the duration of a
// test so color assertions are deterministic regardless of the CI terminal,
// and restores the previous profile on cleanup.
func forceColorProfile() func() {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	return func() { lipgloss.SetColorProfile(prev) }
}

func mkScratch(id, name string, created, expires time.Time, tags []string, ext string, size int64) index.Scratch {
	return index.Scratch{
		ID:        id,
		Name:      name,
		CreatedAt: created,
		ExpiresAt: expires,
		Tags:      tags,
		Ext:       ext,
		Size:      size,
	}
}

func TestPlainTableIsTabSeparatedAndColorless(t *testing.T) {
	now := time.Date(2026, 6, 26, 20, 0, 0, 0, time.UTC)
	scratches := []index.Scratch{
		mkScratch("aaaa", "alpha", now.Add(-2*time.Hour), now.Add(48*time.Hour), []string{"x", "y"}, "md", 1500),
	}

	var buf bytes.Buffer
	if err := Table(&buf, scratches, now, false); err != nil {
		t.Fatalf("Table: %v", err)
	}
	out := buf.String()

	// Header + one row, both tab-separated.
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines (header + row), got %d: %q", len(lines), out)
	}
	header := strings.Split(lines[0], "\t")
	if len(header) != len(columns) {
		t.Errorf("header has %d cols, want %d: %q", len(header), len(columns), lines[0])
	}
	row := strings.Split(lines[1], "\t")
	if len(row) != len(columns) {
		t.Fatalf("row has %d cols, want %d: %q", len(row), len(columns), lines[1])
	}

	// No ANSI escape codes in the plain path.
	if strings.Contains(out, "\x1b[") {
		t.Errorf("plain table must not contain ANSI escapes; got %q", out)
	}

	// Spot-check formatted cells.
	if row[0] != "aaaa" {
		t.Errorf("id cell = %q, want aaaa", row[0])
	}
	if row[1] != "alpha" {
		t.Errorf("name cell = %q, want alpha", row[1])
	}
	if row[2] != "2h" {
		t.Errorf("age cell = %q, want 2h", row[2])
	}
	if row[4] != "x,y" {
		t.Errorf("tags cell = %q, want x,y", row[4])
	}
	if row[5] != "1.5KB" {
		t.Errorf("size cell = %q, want 1.5KB", row[5])
	}
}

func TestEmptyTableMessage(t *testing.T) {
	var buf bytes.Buffer
	if err := Table(&buf, nil, time.Now(), false); err != nil {
		t.Fatalf("Table: %v", err)
	}
	if !strings.Contains(buf.String(), "no scratches yet") {
		t.Errorf("empty table should hint how to create one; got %q", buf.String())
	}
}

func TestColorTableEmitsAnsi(t *testing.T) {
	// lipgloss only emits color when it believes the profile supports it.
	// Force a truecolor profile so the test is deterministic regardless of
	// the CI terminal.
	restore := forceColorProfile()
	defer restore()

	now := time.Date(2026, 6, 26, 20, 0, 0, 0, time.UTC)
	scratches := []index.Scratch{
		mkScratch("bbbb", "beta", now.Add(-time.Hour), now.Add(72*time.Hour), nil, "md", 10),
	}
	var buf bytes.Buffer
	if err := Table(&buf, scratches, now, true); err != nil {
		t.Fatalf("Table: %v", err)
	}
	if !strings.Contains(buf.String(), "\x1b[") {
		t.Errorf("color table should contain ANSI escapes; got %q", buf.String())
	}
}

func TestClassifyBuckets(t *testing.T) {
	now := time.Date(2026, 6, 26, 20, 0, 0, 0, time.UTC)
	cases := []struct {
		name    string
		expires time.Time
		want    lifecycle
	}{
		{"expired-past", now.Add(-time.Minute), expired},
		{"expired-now", now, expired},
		{"soon-1h", now.Add(time.Hour), soon},
		{"soon-edge-24h", now.Add(24 * time.Hour), soon},
		{"fresh-48h", now.Add(48 * time.Hour), fresh},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := classify(index.Scratch{ExpiresAt: c.expires}, now)
			if got != c.want {
				t.Errorf("classify(%s) = %v, want %v", c.name, got, c.want)
			}
		})
	}
}

func TestHumanHelpers(t *testing.T) {
	if got := humanAge(50 * time.Hour); got != "2d" {
		t.Errorf("humanAge(50h) = %q, want 2d", got)
	}
	if got := humanAge(90 * time.Minute); got != "1h" {
		t.Errorf("humanAge(90m) = %q, want 1h", got)
	}
	if got := humanExpiry(-time.Second); got != "expired" {
		t.Errorf("humanExpiry(past) = %q, want expired", got)
	}
	if got := humanExpiry(3 * time.Hour); got != "in 3h" {
		t.Errorf("humanExpiry(3h) = %q, want 'in 3h'", got)
	}
	if got := humanSize(0); got != "0B" {
		t.Errorf("humanSize(0) = %q, want 0B", got)
	}
	if got := humanSize(2048); got != "2.0KB" {
		t.Errorf("humanSize(2048) = %q, want 2.0KB", got)
	}
	if got := nameOrDash(""); got != "-" {
		t.Errorf("nameOrDash(empty) = %q, want -", got)
	}
	if got := tagsOrDash(nil); got != "-" {
		t.Errorf("tagsOrDash(nil) = %q, want -", got)
	}
}

func TestTableSortsNewestFirst(t *testing.T) {
	now := time.Date(2026, 6, 26, 20, 0, 0, 0, time.UTC)
	older := mkScratch("old1", "older", now.Add(-10*time.Hour), now.Add(24*time.Hour), nil, "md", 1)
	newer := mkScratch("new1", "newer", now.Add(-1*time.Hour), now.Add(24*time.Hour), nil, "md", 1)

	var buf bytes.Buffer
	// Pass oldest-first; expect newest-first in output.
	if err := Table(&buf, []index.Scratch{older, newer}, now, false); err != nil {
		t.Fatalf("Table: %v", err)
	}
	out := buf.String()
	iNew := strings.Index(out, "new1")
	iOld := strings.Index(out, "old1")
	if iNew < 0 || iOld < 0 || iNew > iOld {
		t.Errorf("expected newer row before older; got:\n%s", out)
	}
}
