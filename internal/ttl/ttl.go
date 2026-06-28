// Package ttl is scratchpatch's pure time-to-live engine.
//
// It does two things, both as pure functions so the math is trivially
// unit-testable and has no I/O, no clock reads, and no global state:
//
//   - Parse turns a human duration string ("30m", "2h", "7d", "2w", or a
//     composite like "1w2d12h") into a time.Duration. This is the *input*
//     parser the index's on-disk Duration type deliberately doesn't provide:
//     index.Duration handles serialization ("168h0m0s"); ttl.Parse handles the
//     friendly units a person actually types.
//   - Classify buckets a scratch's expiry relative to a supplied "now" into
//     fresh / expiring-soon / expired, so listing and reaping share one
//     definition of those boundaries instead of each rolling their own.
//
// Day and week are treated as fixed spans (24h and 168h). scratchpatch is a
// throwaway-file janitor, not a calendar app — nobody needs DST-correct TTLs on
// a scratch, and fixed spans keep the engine pure and predictable.
package ttl

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// Unit spans. Day and week extend Go's built-in time units (which stop at
// Hour) with the fixed-length spans scratchpatch uses for TTLs.
const (
	Day  = 24 * time.Hour
	Week = 7 * Day
)

// ErrEmptyDuration is returned by Parse when given an empty/whitespace string.
var ErrEmptyDuration = errors.New("empty duration")

// Parse converts a human duration string into a time.Duration.
//
// It accepts the friendly units scratchpatch advertises — s, m, h, d, w — in
// any combination, applied largest-to-smallest by convention but accepted in
// any order: "90m", "1h30m", "7d", "2w", "1w2d12h". A leading sign ("-5m") is
// honored. Bare integers are rejected (a TTL needs a unit) rather than guessed
// at, so "7" is an error instead of silently meaning seconds, minutes, or days.
//
// Parse is the single front door for human durations; callers (the --ttl flag,
// any future --grace flag) should route through it so the accepted vocabulary
// stays consistent.
func Parse(s string) (time.Duration, error) {
	orig := s
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, ErrEmptyDuration
	}

	neg := false
	switch s[0] {
	case '+':
		s = s[1:]
	case '-':
		neg = true
		s = s[1:]
	}
	if s == "" {
		return 0, fmt.Errorf("invalid duration %q: no value after sign", orig)
	}

	var total time.Duration
	// Walk number/unit pairs left to right. We parse the number manually
	// (rather than reaching for strconv per-segment) so the loop stays a single
	// pass and rejects malformed input precisely.
	for i := 0; i < len(s); {
		// Accumulate the numeric run.
		start := i
		var n int64
		for i < len(s) && s[i] >= '0' && s[i] <= '9' {
			d := int64(s[i] - '0')
			// Guard against silly overflow on absurd inputs.
			if n > (1<<62)/10 {
				return 0, fmt.Errorf("invalid duration %q: number too large", orig)
			}
			n = n*10 + d
			i++
		}
		if i == start {
			return 0, fmt.Errorf("invalid duration %q: expected a number at %q", orig, s[start:])
		}
		if i >= len(s) {
			return 0, fmt.Errorf("invalid duration %q: number %d is missing a unit (use s, m, h, d, or w)", orig, n)
		}

		unit, err := unitDuration(s[i])
		if err != nil {
			return 0, fmt.Errorf("invalid duration %q: %w", orig, err)
		}
		total += time.Duration(n) * unit
		i++
	}

	if neg {
		total = -total
	}
	return total, nil
}

// unitDuration maps a single unit byte to its span. Only the friendly units are
// accepted; anything else (including Go's "ns"/"us"/"ms" multi-char units) is
// rejected so the vocabulary stays small and unambiguous for a CLI flag.
func unitDuration(c byte) (time.Duration, error) {
	switch c {
	case 's':
		return time.Second, nil
	case 'm':
		return time.Minute, nil
	case 'h':
		return time.Hour, nil
	case 'd':
		return Day, nil
	case 'w':
		return Week, nil
	default:
		return 0, fmt.Errorf("unknown unit %q (use s, m, h, d, or w)", string(c))
	}
}

// Phase is where a scratch sits on the fresh → expired spectrum.
type Phase int

const (
	// Fresh: comfortably before expiry (more than SoonWindow remaining).
	Fresh Phase = iota
	// Soon: within SoonWindow of expiry, but not yet expired.
	Soon
	// Expired: at or past the expiry instant.
	Expired
)

// String renders a phase as a lowercase label, handy for messages and tests.
func (p Phase) String() string {
	switch p {
	case Fresh:
		return "fresh"
	case Soon:
		return "expiring-soon"
	case Expired:
		return "expired"
	default:
		return "unknown"
	}
}

// SoonWindow is the lead time before expiry within which a scratch is
// classified Soon. It's the single definition of "expiring soon" that both the
// table renderer and the reaper can lean on.
const SoonWindow = 24 * time.Hour

// Classify buckets an expiry instant relative to now.
//
// The boundaries are deliberate and tested: a scratch is Expired the instant
// now reaches expiresAt (expiry is inclusive — at the deadline you're done),
// and Soon the instant the remaining time drops to SoonWindow or less. now is a
// parameter, never time.Now(), so classification is deterministic.
func Classify(expiresAt, now time.Time) Phase {
	if !now.Before(expiresAt) {
		return Expired
	}
	if expiresAt.Sub(now) <= SoonWindow {
		return Soon
	}
	return Fresh
}

// IsExpired reports whether expiresAt is at or before now. It's the predicate
// the reaper uses to decide which live scratches to sweep into the morgue, kept
// alongside Classify so "expired" means exactly one thing across the codebase.
func IsExpired(expiresAt, now time.Time) bool {
	return !now.Before(expiresAt)
}
