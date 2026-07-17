// M5 reaping: the one place scratchpatch is allowed to destroy content.
//
// reap is a two-stage sweep, and it honors scratchpatch's core safety rule —
// "nothing is ever lost in one step" — by never doing both stages to the same
// scratch in a single pass:
//
//  1. Expired *live* scratches are soft-deleted into the morgue (reusing the
//     M4 MoveToMorgue path, so the index/filesystem stay in lockstep). A
//     scratch that expires today is merely moved today; it isn't eligible for
//     hard-deletion until it has aged the grace window *in the morgue*.
//  2. Morgue items whose grace window has elapsed are hard-deleted for good.
//
// HardDelete is intentionally the only content-destroying operation in the
// whole codebase, and it refuses to touch anything that isn't both in the
// morgue and past its grace deadline — belt-and-suspenders against a caller (or
// a future bug) trying to nuke a live or still-in-grace scratch.
package store

import (
	"fmt"
	"os"
	"time"

	"github.com/rwrife/scratchpatch/internal/index"
)

// HardDelete permanently removes a scratch: it deletes the content file from
// the morgue and drops the record from the index. It is the only operation in
// scratchpatch that destroys content, so it guards itself rather than trusting
// the caller:
//
//   - the scratch must be in the morgue (a live scratch can never be hard-
//     deleted; it has to be soft-deleted first), and
//   - it must be at or past its purge deadline (DeletedAt + grace).
//
// now is supplied (not read from the clock) so the grace check is deterministic
// and testable. The content file is removed first; only on success is the index
// entry dropped, mirroring the move operations' filesystem-first ordering. A
// missing content file is tolerated (treated as already-gone) so a half-purged
// scratch can still be cleaned out of the index.
func (s *Store) HardDelete(sc index.Scratch, now time.Time) error {
	if !sc.Morgued() {
		return fmt.Errorf("refusing to hard-delete %s: it is live, not in the morgue", sc.ID)
	}
	purgeAt, _ := s.PurgeAt(sc)
	if now.Before(purgeAt) {
		return fmt.Errorf("refusing to hard-delete %s: still within the grace window (purges %s)",
			sc.ID, purgeAt.Format(time.RFC3339))
	}

	path := s.morguePath(sc)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("purge scratch %s content: %w", sc.ID, err)
	}
	if err := s.idx.Delete(sc.ID); err != nil {
		return fmt.Errorf("drop scratch %s from index: %w", sc.ID, err)
	}
	return nil
}

// ReapPlan is the result of a reap: what was (or, in dry-run, would be) swept
// to the morgue and what was hard-deleted. The CLI renders this; tests assert
// on it. Slices preserve the order the store returned (newest-first).
type ReapPlan struct {
	// Morgued are the expired live scratches moved (or to-be-moved) into the
	// morgue. Their DeletedAt is set on the returned records when not a dry-run.
	Morgued []index.Scratch

	// Purged are the morgue items past their grace window that were (or would
	// be) hard-deleted.
	Purged []index.Scratch

	// PinnedSkipped counts expired live scratches that reap left alone because
	// they are pinned. They would otherwise have been swept to the morgue; the
	// count lets the CLI report "skipped N pinned" so the exemption is visible.
	PinnedSkipped int

	// DryRun records whether this plan was computed without making changes.
	DryRun bool
}

// Empty reports whether the reap had nothing to do — useful for a tidy
// "nothing to reap" message.
func (p ReapPlan) Empty() bool { return len(p.Morgued) == 0 && len(p.Purged) == 0 }

// Reap performs the two-stage sweep described in the package doc and returns a
// ReapPlan describing what happened.
//
// When dryRun is true, nothing on disk or in the index changes: Reap classifies
// the store and returns the plan it *would* execute, so `sp reap --dry-run`
// shows exactly what is about to die. The classification uses the same
// now-relative predicates the rest of scratchpatch uses (ttl.IsExpired for
// stage 1, the PurgeAt deadline for stage 2), so the dry-run can never disagree
// with the real thing.
//
// Ordering matters: stage 1 (expired live → morgue) runs first and stage 2
// (past-grace morgue → purge) operates only on the *pre-existing* morgue set.
// A scratch swept in during this same call is therefore never also purged in
// the same call — its grace clock starts now. This is the single-step-safety
// rule made literal.
func (s *Store) Reap(now time.Time, dryRun bool) (ReapPlan, error) {
	plan := ReapPlan{DryRun: dryRun}

	live, err := s.ListLive()
	if err != nil {
		return ReapPlan{}, err
	}
	morgue, err := s.ListMorgue()
	if err != nil {
		return ReapPlan{}, err
	}

	// Stage 1: expired live scratches → morgue. Pinned scratches are exempt:
	// even when expired they stay live, and we tally them so the summary can
	// report how many the pin spared.
	for _, sc := range live {
		if !isExpired(sc, now) {
			continue
		}
		if sc.Pinned {
			plan.PinnedSkipped++
			continue
		}
		if dryRun {
			plan.Morgued = append(plan.Morgued, sc)
			continue
		}
		moved, err := s.MoveToMorgue(sc)
		if err != nil {
			return plan, fmt.Errorf("reap: sweep %s to morgue: %w", sc.ID, err)
		}
		plan.Morgued = append(plan.Morgued, moved)
	}

	// Stage 2: morgue items past their grace deadline → hard-deleted. Only the
	// morgue set as it existed *before* this reap is considered, so freshly
	// swept scratches get their full grace window.
	for _, sc := range morgue {
		purgeAt, ok := s.PurgeAt(sc)
		if !ok || now.Before(purgeAt) {
			continue
		}
		if dryRun {
			plan.Purged = append(plan.Purged, sc)
			continue
		}
		if err := s.HardDelete(sc, now); err != nil {
			return plan, fmt.Errorf("reap: %w", err)
		}
		plan.Purged = append(plan.Purged, sc)
	}

	return plan, nil
}

// isExpired reports whether a scratch's expiry deadline has been reached. It's
// a thin store-local wrapper so reap doesn't import ttl directly for a one-line
// comparison while still using the same inclusive-at-the-deadline semantics
// (now is not before ExpiresAt).
func isExpired(sc index.Scratch, now time.Time) bool {
	return !now.Before(sc.ExpiresAt)
}
