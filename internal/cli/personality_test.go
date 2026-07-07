package cli

import (
	"strings"
	"testing"
	"time"
)

// The M6 polish pass gives the direct command confirmation lines the same
// tombstone-flavored voice the render layer already had. These tests pin that
// flavor in place (so it can't silently regress to clinical wording) while also
// guarding the stable substrings other tests and scripts rely on.

func TestNewConfirmationHasFlavorAndLifespan(t *testing.T) {
	s := newSession(t)
	out, err := s.run("new", "note", "--no-edit", "--ext", "txt", "--ttl", "7d")
	if err != nil {
		t.Fatalf("new: %v (out=%s)", err, out)
	}
	// Stable anchor other tests depend on.
	if !strings.Contains(out, "created scratch") {
		t.Errorf("new should confirm creation with the stable anchor; got %q", out)
	}
	// Flavor: a freshly created scratch is reminded of its mortality.
	if !strings.Contains(out, "borrowed time") {
		t.Errorf("new should note the scratch's borrowed time; got %q", out)
	}
	// And the countdown should reflect a multi-day TTL. Measured from "now",
	// a 7d TTL can floor to ~6d after the sub-second elapsed, so assert on the
	// day-granularity shape rather than a brittle exact count.
	if !strings.Contains(out, "expires in ~") || !strings.HasSuffix(strings.TrimSpace(out), "d") {
		t.Errorf("new should surface a multi-day expiry countdown; got %q", out)
	}
}

func TestOpenConfirmationHasFlavor(t *testing.T) {
	s := newSession(t)
	id := s.newScratchID("reopen-me")
	// Use a no-op editor so open takes the success path and prints its
	// confirmation line (with EDITOR unset it would fall back to printing the
	// path instead).
	t.Setenv("EDITOR", "true")
	out, err := s.run("open", id)
	if err != nil {
		t.Fatalf("open: %v (out=%s)", err, out)
	}
	if !strings.Contains(out, "opened scratch") {
		t.Errorf("open should confirm with the stable anchor; got %q", out)
	}
	if !strings.Contains(out, "slab") {
		t.Errorf("open should carry the tombstone flavor; got %q", out)
	}
}

func TestRmAndResurrectKeepAnchorsAndFlavor(t *testing.T) {
	s := newSession(t)
	id := s.newScratchID("cycle")

	rmOut, err := s.run("rm", id)
	if err != nil {
		t.Fatalf("rm: %v (out=%s)", err, rmOut)
	}
	// Anchor: existing tests assert on "morgue".
	if !strings.Contains(rmOut, "morgue") {
		t.Errorf("rm should mention the morgue; got %q", rmOut)
	}
	// Flavor + the restore hint must survive.
	if !strings.Contains(rmOut, "buried") || !strings.Contains(rmOut, "sp resurrect") {
		t.Errorf("rm should be flavored and still hint at restore; got %q", rmOut)
	}

	resOut, err := s.run("resurrect", id)
	if err != nil {
		t.Fatalf("resurrect: %v (out=%s)", err, resOut)
	}
	// Anchor: existing tests assert on "live again".
	if !strings.Contains(resOut, "live again") {
		t.Errorf("resurrect should confirm the scratch is live again; got %q", resOut)
	}
	if !strings.Contains(resOut, "morgue") {
		t.Errorf("resurrect should reference clawing out of the morgue; got %q", resOut)
	}
}

func TestHumanCountdownWording(t *testing.T) {
	cases := []struct {
		name string
		d    time.Duration
		want string
	}{
		{"days", 7 * 24 * time.Hour, "expires in ~7d"},
		{"hours", 5 * time.Hour, "expires in ~5h"},
		{"minutes", 12 * time.Minute, "expires in ~12m"},
		{"sub-minute", 20 * time.Second, "expires within the minute"},
		{"already-due", -time.Hour, "already due for the reaper"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := humanCountdown(tc.d); !strings.Contains(got, tc.want) {
				t.Errorf("humanCountdown(%v) = %q, want it to contain %q", tc.d, got, tc.want)
			}
		})
	}
}

func TestLifespanNoteFallsBackWithoutExpiry(t *testing.T) {
	// Defensive path: a zero expiry (shouldn't happen for real scratches)
	// still yields a sensible, non-panicking line.
	got := lifespanNote(time.Time{}, time.Now())
	if !strings.Contains(got, "reap it") {
		t.Errorf("lifespanNote with no expiry should mention reaping; got %q", got)
	}
}
