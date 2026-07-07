package picker

import (
	"testing"
	"time"

	"github.com/rwrife/scratchpatch/internal/index"
)

// cand is a tiny constructor for tests: it builds a Candidate from an id, name,
// and tags, using the id as the label (labels are opaque to matching, so the
// exact string doesn't matter here — only the derived haystack does).
func cand(id, name string, tags ...string) Candidate {
	sc := index.Scratch{ID: id, Name: name, Tags: tags, CreatedAt: time.Now()}
	return NewCandidate(sc, id+" "+name)
}

// ids extracts the scratch ids from a candidate slice, in order, so tests can
// assert on ranking with a compact []string comparison.
func ids(cands []Candidate) []string {
	out := make([]string, len(cands))
	for i, c := range cands {
		out[i] = c.Scratch.ID
	}
	return out
}

func TestFilterEmptyQueryReturnsAllInOrder(t *testing.T) {
	in := []Candidate{cand("aaa1", "alpha"), cand("bbb2", "beta"), cand("ccc3", "gamma")}
	got := Filter(in, "   ")
	if want := []string{"aaa1", "bbb2", "ccc3"}; !equal(ids(got), want) {
		t.Errorf("empty query should return all in input order; got %v want %v", ids(got), want)
	}
	// The input slice must not be mutated.
	if in[0].Scratch.ID != "aaa1" {
		t.Error("Filter mutated its input slice")
	}
}

func TestFilterMatchesByNameSubsequence(t *testing.T) {
	in := []Candidate{cand("1", "todo-list"), cand("2", "README"), cand("3", "grocery")}
	got := Filter(in, "tdo") // subsequence of "todo"
	if len(got) != 1 || got[0].Scratch.ID != "1" {
		t.Fatalf("expected only todo-list to match \"tdo\"; got %v", ids(got))
	}
}

func TestFilterMatchesByIDAndTag(t *testing.T) {
	in := []Candidate{
		cand("deadbeef", "notes"),
		cand("cafef00d", "budget", "finance", "q3"),
	}
	// Match by a slice of the id.
	if got := Filter(in, "beef"); len(got) != 1 || got[0].Scratch.ID != "deadbeef" {
		t.Errorf("id substring should match; got %v", ids(got))
	}
	// Match by a tag.
	if got := Filter(in, "finance"); len(got) != 1 || got[0].Scratch.ID != "cafef00d" {
		t.Errorf("tag should be searchable; got %v", ids(got))
	}
}

func TestFilterIsCaseInsensitive(t *testing.T) {
	in := []Candidate{cand("1", "MyNotes")}
	if got := Filter(in, "mynotes"); len(got) != 1 {
		t.Errorf("matching should ignore case; got %v", ids(got))
	}
}

func TestFilterRanksContiguousAndBoundaryHigher(t *testing.T) {
	// "todo" appears as a clean word-start run in the first, and only as a
	// scattered subsequence in the second, so the first should rank ahead.
	in := []Candidate{
		cand("2", "t-o-d-o-scattered"), // t...o...d...o but broken up
		cand("1", "todo-list"),         // contiguous, at the start
	}
	got := Filter(in, "todo")
	if len(got) != 2 {
		t.Fatalf("both should match \"todo\"; got %v", ids(got))
	}
	if got[0].Scratch.ID != "1" {
		t.Errorf("contiguous word-boundary match should rank first; got order %v", ids(got))
	}
}

func TestFilterNoMatchReturnsEmpty(t *testing.T) {
	in := []Candidate{cand("1", "alpha"), cand("2", "beta")}
	if got := Filter(in, "zzz"); len(got) != 0 {
		t.Errorf("a query with no subsequence match should return empty; got %v", ids(got))
	}
}

func TestFilterQueryLongerThanHaystackDoesNotMatch(t *testing.T) {
	in := []Candidate{cand("ab", "x")} // haystack "ab x" — shorter than the query
	if got := Filter(in, "abcdefghij"); len(got) != 0 {
		t.Errorf("an over-long query cannot match; got %v", ids(got))
	}
}

func TestFuzzyScoreOrderMatters(t *testing.T) {
	// "ba" is not a subsequence of "abc" (b comes after a, but a-then-b order
	// fails since there's no b after the a? actually a,b are in order) — use a
	// clearer non-match: "ca" is not a subsequence of "abc".
	if _, ok := fuzzyScore("abc", "ca"); ok {
		t.Error(`"ca" should not be a subsequence of "abc"`)
	}
	if _, ok := fuzzyScore("abc", "ac"); !ok {
		t.Error(`"ac" should be a subsequence of "abc"`)
	}
}

// equal compares two string slices element-wise.
func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
