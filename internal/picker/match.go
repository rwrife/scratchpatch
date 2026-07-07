// Package picker turns "which scratch did you mean?" into a small, testable
// core plus a couple of thin front-ends.
//
// The heart of the package is pure and I/O-free: given the live scratches and a
// query string, Filter ranks them by a subsequence fuzzy match so the caller
// can show the best candidates first. The interactive front-ends (an
// fzf hand-off and a built-in numbered/filter prompt) live alongside it but keep
// all their terminal and process concerns out of the matcher, so ranking stays
// trivially unit-testable.
//
// picker never opens anything itself. It only decides *which* scratch the user
// picked and hands that back; `sp open` owns the $EDITOR launch, exactly as it
// does for an explicit id. That keeps the "one place opens editors" boundary
// (new.go's openInEditor) intact.
package picker

import (
	"sort"
	"strings"

	"github.com/rwrife/scratchpatch/internal/index"
)

// Candidate is a scratch paired with everything the picker needs to display and
// rank it, flattened so the picker never reaches back into the store. label is
// the human line shown in a prompt; haystack is the lowercased text a query is
// matched against (id + name + tags), so filtering by any of those just works.
type Candidate struct {
	Scratch  index.Scratch
	Label    string
	haystack string
	score    int
}

// NewCandidate builds a Candidate from a scratch and its pre-rendered display
// label. The label is supplied by the caller (render owns presentation), while
// the match haystack is derived here from the fields a user is likely to type:
// the id, the name, and any tags.
func NewCandidate(sc index.Scratch, label string) Candidate {
	parts := make([]string, 0, 2+len(sc.Tags))
	parts = append(parts, sc.ID, sc.Name)
	parts = append(parts, sc.Tags...)
	return Candidate{
		Scratch:  sc,
		Label:    label,
		haystack: strings.ToLower(strings.Join(parts, " ")),
	}
}

// Filter returns the candidates whose haystack fuzzily matches query, best
// match first. An empty (or whitespace-only) query matches everything and
// preserves the input order, so it doubles as "show me all of them". Matching
// is case-insensitive subsequence matching: the query's characters must appear
// in order but not necessarily adjacently, the same feel as fzf, so "tdo"
// matches "todo".
//
// The input slice is never mutated; a new, ranked slice is returned.
func Filter(cands []Candidate, query string) []Candidate {
	q := strings.ToLower(strings.TrimSpace(query))

	if q == "" {
		out := make([]Candidate, len(cands))
		copy(out, cands)
		return out
	}

	type ranked struct {
		cand Candidate
		idx  int // original position, for a stable tie-break
	}
	var hits []ranked
	for i, c := range cands {
		score, ok := fuzzyScore(c.haystack, q)
		if !ok {
			continue
		}
		c.score = score
		hits = append(hits, ranked{cand: c, idx: i})
	}

	// Higher score first; ties fall back to original order so the result is
	// deterministic and the newest-first listing order shows through.
	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].cand.score != hits[j].cand.score {
			return hits[i].cand.score > hits[j].cand.score
		}
		return hits[i].idx < hits[j].idx
	})

	out := make([]Candidate, len(hits))
	for i, h := range hits {
		out[i] = h.cand
	}
	return out
}

// fuzzyScore reports whether query is a subsequence of haystack and, if so, how
// good a match it is. Both are expected to already be lowercased. The score
// rewards matches that are contiguous and that land at word boundaries, so a
// tight, prefix-y hit ("todo" in "todo-list") outranks a scattered one ("tol"
// in "trouble-loop"). A query longer than the haystack, or one whose characters
// don't all appear in order, is not a match.
func fuzzyScore(haystack, query string) (int, bool) {
	if query == "" {
		return 0, true
	}
	if len(query) > len(haystack) {
		return 0, false
	}

	hs := []rune(haystack)
	qs := []rune(query)

	score := 0
	qi := 0
	prevMatch := -2 // so the first match is never counted as "adjacent"
	for hi := 0; hi < len(hs) && qi < len(qs); hi++ {
		if hs[hi] != qs[qi] {
			continue
		}
		// Base point for the matched character.
		score++
		// Bonus for consecutive matches — contiguous runs feel like the
		// "real" match a user typed.
		if hi == prevMatch+1 {
			score += 3
		}
		// Bonus for matching at the start or just after a separator, which is
		// where meaningful words begin (id start, name start, tag start).
		if hi == 0 || isBoundary(hs[hi-1]) {
			score += 2
		}
		prevMatch = hi
		qi++
	}

	if qi != len(qs) {
		return 0, false // ran out of haystack before consuming the query
	}
	return score, true
}

// isBoundary reports whether r is the kind of character that precedes the start
// of a new "word" in a scratch's searchable text, so a match right after it
// earns the word-boundary bonus.
func isBoundary(r rune) bool {
	switch r {
	case ' ', '-', '_', '.', '/', ':':
		return true
	default:
		return false
	}
}
