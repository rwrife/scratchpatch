package ttl

import (
	"errors"
	"testing"
	"time"
)

func TestParse(t *testing.T) {
	cases := []struct {
		in   string
		want time.Duration
	}{
		// Single units.
		{"30s", 30 * time.Second},
		{"5m", 5 * time.Minute},
		{"2h", 2 * time.Hour},
		{"7d", 7 * Day},
		{"2w", 2 * Week},
		{"0d", 0},

		// Composites in canonical (descending) order.
		{"1h30m", 90 * time.Minute},
		{"1w2d12h", Week + 2*Day + 12*time.Hour},
		{"1d1h1m1s", Day + time.Hour + time.Minute + time.Second},

		// Composites in non-canonical order still sum.
		{"30m1h", 90 * time.Minute},
		{"12h2d", 2*Day + 12*time.Hour},

		// Repeated units accumulate (we sum, we don't dedupe).
		{"1h1h", 2 * time.Hour},

		// Whitespace is trimmed.
		{"  3d  ", 3 * Day},

		// Signs.
		{"+5m", 5 * time.Minute},
		{"-5m", -5 * time.Minute},
		{"-1d", -Day},

		// Multi-digit numbers.
		{"168h", 168 * time.Hour},
		{"90m", 90 * time.Minute},
		{"100d", 100 * Day},
	}
	for _, c := range cases {
		got, err := Parse(c.in)
		if err != nil {
			t.Errorf("Parse(%q) unexpected error: %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("Parse(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestParse7dEqualsWeekDiv(t *testing.T) {
	// Sanity: a week is exactly seven days, and the default 7d matches the
	// config default TTL (168h) so the two never silently diverge.
	d, err := Parse("7d")
	if err != nil {
		t.Fatalf("Parse(7d): %v", err)
	}
	if d != Week {
		t.Fatalf("7d = %v, want one week %v", d, Week)
	}
	if d != 168*time.Hour {
		t.Fatalf("7d = %v, want 168h", d)
	}
}

func TestParseErrors(t *testing.T) {
	bad := []string{
		"",     // empty
		"   ",  // whitespace only
		"7",    // bare number, no unit
		"d",    // unit with no number
		"7x",   // unknown unit
		"7dd",  // unit-with-no-number after a valid pair
		"1h30", // trailing number with no unit
		"-",    // sign only
		"+",    // sign only
		"1.5h", // no fractional support (a scratch TTL doesn't need it)
		"ten",  // not numeric
		"7 d",  // internal space is not allowed
		"1ms",  // Go's sub-units are intentionally rejected
		"1us",  // ditto
		"abch", // junk before a unit
	}
	for _, in := range bad {
		if _, err := Parse(in); err == nil {
			t.Errorf("Parse(%q) = nil error, want an error", in)
		}
	}
}

func TestParseEmptyIsSentinel(t *testing.T) {
	if _, err := Parse(""); !errors.Is(err, ErrEmptyDuration) {
		t.Fatalf("Parse(\"\") error = %v, want ErrEmptyDuration", err)
	}
	if _, err := Parse("   "); !errors.Is(err, ErrEmptyDuration) {
		t.Fatalf("Parse(\"   \") error = %v, want ErrEmptyDuration", err)
	}
}

func TestClassifyBoundaries(t *testing.T) {
	now := time.Date(2026, 6, 28, 12, 0, 0, 0, time.UTC)

	cases := []struct {
		name    string
		expires time.Time
		want    Phase
	}{
		{"far future is fresh", now.Add(72 * time.Hour), Fresh},
		{"just over the soon window is fresh", now.Add(SoonWindow + time.Second), Fresh},
		{"exactly at the soon window is soon", now.Add(SoonWindow), Soon},
		{"within the soon window is soon", now.Add(time.Hour), Soon},
		{"one second from expiry is soon", now.Add(time.Second), Soon},
		{"exactly at expiry is expired", now, Expired},
		{"past expiry is expired", now.Add(-time.Second), Expired},
		{"long past expiry is expired", now.Add(-72 * time.Hour), Expired},
	}
	for _, c := range cases {
		if got := Classify(c.expires, now); got != c.want {
			t.Errorf("%s: Classify = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestIsExpired(t *testing.T) {
	now := time.Date(2026, 6, 28, 12, 0, 0, 0, time.UTC)
	if IsExpired(now.Add(time.Nanosecond), now) {
		t.Error("a scratch expiring one ns in the future must not be expired")
	}
	if !IsExpired(now, now) {
		t.Error("expiry is inclusive: at the deadline a scratch is expired")
	}
	if !IsExpired(now.Add(-time.Hour), now) {
		t.Error("a past deadline must be expired")
	}
}

func TestPhaseString(t *testing.T) {
	cases := map[Phase]string{
		Fresh:     "fresh",
		Soon:      "expiring-soon",
		Expired:   "expired",
		Phase(99): "unknown",
	}
	for p, want := range cases {
		if got := p.String(); got != want {
			t.Errorf("Phase(%d).String() = %q, want %q", int(p), got, want)
		}
	}
}

// TestClassifyConsistentWithIsExpired guards the invariant that Classify and
// IsExpired agree on the Expired boundary — they must, since the reaper trusts
// IsExpired and the table trusts Classify.
func TestClassifyConsistentWithIsExpired(t *testing.T) {
	now := time.Date(2026, 6, 28, 12, 0, 0, 0, time.UTC)
	offsets := []time.Duration{-time.Hour, -time.Second, 0, time.Second, time.Hour, 48 * time.Hour}
	for _, off := range offsets {
		exp := now.Add(off)
		classified := Classify(exp, now) == Expired
		if classified != IsExpired(exp, now) {
			t.Errorf("offset %v: Classify says expired=%v but IsExpired=%v",
				off, classified, IsExpired(exp, now))
		}
	}
}
