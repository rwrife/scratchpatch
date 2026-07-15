// stats.go computes fun-but-useful metrics about the store: how much rot the
// morgue is holding for you, your oldest surviving scratch, the bytes that have
// passed through instead of rotting in /tmp, and a tag breakdown.
//
// Like doctor, Stats is read-only: it derives everything from the index (and
// the record sizes it already carries) without touching content or adding new
// persistent counters. scratchpatch does not track lifetime create/reap totals
// on disk in v0.1, so this reports what the index can honestly account for and
// labels nothing it can't measure — the "bytes that passed through the store"
// number is the live + morgue footprint the index currently knows about, not a
// fabricated all-time tally.
package store

import (
	"sort"
	"time"

	"github.com/rwrife/scratchpatch/internal/index"
)

// TagCount is a single tag and how many scratches carry it, used for the
// top-N breakdown in the stats report.
type TagCount struct {
	Tag   string
	Count int
}

// Stats is the read-only metrics snapshot `sp stats` reports. It is plain data;
// the render layer decides how to phrase and color it. Times/durations are
// captured as of the moment Stats was computed.
type Stats struct {
	// LiveCount / LiveBytes are the count and total content size of live
	// scratches (the current footprint the reaper hasn't touched).
	LiveCount int
	LiveBytes int64

	// MorgueCount / MorgueBytes are the count and total size of soft-deleted
	// scratches still recoverable in the morgue.
	MorgueCount int
	MorgueBytes int64

	// PurgeableCount is how many morgue items are already past their grace
	// window and would be hard-deleted on the next `sp reap`.
	PurgeableCount int

	// TotalBytes is LiveBytes + MorgueBytes: the bytes currently held by the
	// store — i.e. what has passed through instead of rotting loose in /tmp,
	// as far as the index can account for.
	TotalBytes int64

	// OldestID / OldestName / OldestAge describe the oldest living scratch
	// ("oldest survivor"). OldestID is empty when there are no live scratches.
	OldestID   string
	OldestName string
	OldestAge  time.Duration

	// Tags is the top tags by count, most-common first (ties broken
	// alphabetically). Empty when no live scratch carries a tag.
	Tags []TagCount

	// Grace is the configured morgue grace window, surfaced so the report can
	// contextualize PurgeableCount.
	Grace time.Duration
}

// topTags is how many entries the tag breakdown keeps. Small on purpose — this
// is a glanceable stat line, not a full census.
const topTags = 5

// Stats computes the store's metrics from the index as of now. It is read-only
// and never changes the store. Live vs morgue is decided per record; sizes come
// from the size the index recorded at last write.
func (s *Store) Stats() (Stats, error) {
	all, err := s.idx.List()
	if err != nil {
		return Stats{}, err
	}

	now := time.Now()
	st := Stats{Grace: s.cfg.Grace}
	tagHits := make(map[string]int)

	var oldest index.Scratch
	haveOldest := false

	for _, sc := range all {
		if sc.Morgued() {
			st.MorgueCount++
			st.MorgueBytes += sc.Size
			if !now.Before(sc.DeletedAt.Add(s.cfg.Grace)) {
				st.PurgeableCount++
			}
			continue
		}

		// Live scratch: count it, size it, tag it, and track the oldest.
		st.LiveCount++
		st.LiveBytes += sc.Size
		for _, t := range sc.Tags {
			tagHits[t]++
		}
		if !haveOldest || sc.CreatedAt.Before(oldest.CreatedAt) {
			oldest = sc
			haveOldest = true
		}
	}

	st.TotalBytes = st.LiveBytes + st.MorgueBytes

	if haveOldest {
		st.OldestID = oldest.ID
		st.OldestName = oldest.Name
		st.OldestAge = now.Sub(oldest.CreatedAt)
	}

	st.Tags = topTagCounts(tagHits)
	return st, nil
}

// topTagCounts turns a tag→count map into the top-N slice, ordered by count
// desc then tag asc for a stable, sensible ranking.
func topTagCounts(hits map[string]int) []TagCount {
	if len(hits) == 0 {
		return nil
	}
	counts := make([]TagCount, 0, len(hits))
	for tag, n := range hits {
		counts = append(counts, TagCount{Tag: tag, Count: n})
	}
	sort.SliceStable(counts, func(i, j int) bool {
		if counts[i].Count != counts[j].Count {
			return counts[i].Count > counts[j].Count
		}
		return counts[i].Tag < counts[j].Tag
	})
	if len(counts) > topTags {
		counts = counts[:topTags]
	}
	return counts
}
