// Content-hash duplicate detection: the store's "you already scratched this"
// hygiene primitive.
//
// Throwaway files breed byte-identical copies — the same log pasted three
// times, an agent re-capturing identical output on every run, the same snippet
// `sp new`'d across two projects. Each copy ages out separately and wastes
// footprint. dedup hashes every live scratch's content, groups by digest, and
// reports the clusters of identical scratches; `--collapse` (opt-in) moves the
// redundant copies to the morgue, always keeping the oldest as canonical.
//
// dedup is distinct from doctor (which reconciles index-vs-disk drift, not
// content equality) and reap (which is TTL-based). Like every other operation
// here it is morgue-first: the collapse path never hard-deletes, and the
// default path never moves anything at all.
package store

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"sort"

	"github.com/rwrife/scratchpatch/internal/index"
)

// DupMember is one scratch within a duplicate cluster, carrying the metadata
// the report needs without exposing the whole index record. Canonical marks the
// member the store would keep (the oldest) so the render layer doesn't have to
// re-derive it.
type DupMember struct {
	Scratch   index.Scratch
	Canonical bool
}

// DupCluster is a group of live scratches whose content hashes to the same
// digest: two or more byte-identical copies. Members are ordered
// canonical-first (oldest), then newest-first for the redundant copies, so the
// report reads top-down as "keep this, these are the extras". WastedBytes is the
// footprint the redundant copies cost (every member past the canonical one),
// i.e. what `--collapse` would reclaim from the live set.
type DupCluster struct {
	Digest      string
	Members     []DupMember
	WastedBytes int64
}

// Canonical returns the cluster's keep-member (the oldest). A well-formed
// cluster always has at least two members, so Members[0] is safe.
func (c DupCluster) Canonical() index.Scratch { return c.Members[0].Scratch }

// Redundant returns the non-canonical members: the copies `--collapse` would
// move to the morgue.
func (c DupCluster) Redundant() []DupMember { return c.Members[1:] }

// DedupReport is the full read-only result of scanning the live set for exact
// duplicates. Clusters is empty when the store is unique. TotalWasted is the
// sum of every cluster's WastedBytes — the bytes reclaimable by collapsing.
// ScannedCount is how many live scratches were hashed, so the report can say
// "scanned N, found M clusters".
type DedupReport struct {
	Clusters     []DupCluster
	TotalWasted  int64
	ScannedCount int
}

// Clean reports whether no duplicates were found.
func (r DedupReport) Clean() bool { return len(r.Clusters) == 0 }

// Dedup hashes the content of every live scratch and groups byte-identical
// copies into clusters. It is strictly read-only — nothing is moved or deleted.
//
// A scratch whose content file can't be read (an orphaned/missing entry doctor
// would flag) is skipped rather than aborting the scan, so one bad file doesn't
// hide every real duplicate; the skip is surfaced via the returned error only
// if reading the index itself fails. Content equality is by digest alone, so
// identical bytes cluster together regardless of differing names, tags, or ext.
func (s *Store) Dedup() (DedupReport, error) {
	live, err := s.ListLive()
	if err != nil {
		return DedupReport{}, fmt.Errorf("dedup: list live: %w", err)
	}

	// groups maps a content digest → the scratches that hashed to it, in
	// index order (newest-first, per ListLive).
	groups := make(map[string][]index.Scratch)
	var order []string // first-seen digest order, for deterministic output
	scanned := 0

	for _, sc := range live {
		b, err := os.ReadFile(s.LivePath(sc))
		if err != nil {
			// Unreadable/missing content: skip it. doctor is the tool that
			// reports such drift; dedup shouldn't crash on it.
			continue
		}
		scanned++
		sum := sha256.Sum256(b)
		digest := hex.EncodeToString(sum[:])
		if _, ok := groups[digest]; !ok {
			order = append(order, digest)
		}
		groups[digest] = append(groups[digest], sc)
	}

	var report DedupReport
	report.ScannedCount = scanned

	for _, digest := range order {
		members := groups[digest]
		if len(members) < 2 {
			continue // unique content is not a cluster
		}
		cluster := buildCluster(digest, members)
		report.Clusters = append(report.Clusters, cluster)
		report.TotalWasted += cluster.WastedBytes
	}

	// Deterministic cluster order: most-wasteful first, ties broken by the
	// canonical member's id so runs and tests are stable.
	sort.SliceStable(report.Clusters, func(i, j int) bool {
		if report.Clusters[i].WastedBytes != report.Clusters[j].WastedBytes {
			return report.Clusters[i].WastedBytes > report.Clusters[j].WastedBytes
		}
		return report.Clusters[i].Canonical().ID < report.Clusters[j].Canonical().ID
	})

	return report, nil
}

// buildCluster orders a set of same-digest scratches into a DupCluster: the
// oldest (by CreatedAt, ties broken by id) is the canonical keep-member, and the
// rest are the redundant copies ordered newest-first. WastedBytes sums the sizes
// of every redundant member.
func buildCluster(digest string, members []index.Scratch) DupCluster {
	ordered := make([]index.Scratch, len(members))
	copy(ordered, members)
	// Oldest first so ordered[0] is canonical.
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].CreatedAt.Equal(ordered[j].CreatedAt) {
			return ordered[i].ID < ordered[j].ID
		}
		return ordered[i].CreatedAt.Before(ordered[j].CreatedAt)
	})

	cluster := DupCluster{Digest: digest}
	for i, sc := range ordered {
		canonical := i == 0
		cluster.Members = append(cluster.Members, DupMember{Scratch: sc, Canonical: canonical})
		if !canonical {
			cluster.WastedBytes += sc.Size
		}
	}
	return cluster
}

// CollapseResult records what a collapse run moved to the morgue: the ids that
// were morgued and the bytes reclaimed from the live set. Kept as plain data so
// the render layer can report it without re-deriving anything.
type CollapseResult struct {
	// MovedIDs are the redundant scratch ids that were moved to the morgue,
	// in cluster/order.
	MovedIDs []string
	// ReclaimedBytes is the total size of the moved copies.
	ReclaimedBytes int64
}

// Collapse moves the redundant copies in report's clusters to the morgue,
// keeping each cluster's canonical (oldest) member live. It never hard-deletes:
// collapsed copies land in the morgue exactly like `sp rm`, recoverable with
// `sp resurrect` until the reaper purges them past the grace window.
//
// Collapse re-reads nothing and trusts the report it's handed — callers pass the
// report from a preceding Dedup() so the "what will move" preview and the actual
// move agree. Each move goes through MoveToMorgue, so index/filesystem stay in
// lockstep; a failure on any member aborts with what moved so far reported via
// the returned CollapseResult (already-moved copies stay morgued and recoverable).
func (s *Store) Collapse(report DedupReport) (CollapseResult, error) {
	var result CollapseResult
	for _, cluster := range report.Clusters {
		for _, m := range cluster.Redundant() {
			moved, err := s.MoveToMorgue(m.Scratch)
			if err != nil {
				return result, fmt.Errorf("collapse %s: %w", m.Scratch.ID, err)
			}
			result.MovedIDs = append(result.MovedIDs, moved.ID)
			result.ReclaimedBytes += m.Scratch.Size
		}
	}
	return result, nil
}
