package picker

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/rwrife/scratchpatch/internal/index"
)

// threeCands builds a small, stable candidate set for the front-end tests.
func threeCands() []Candidate {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	mk := func(id, name string) Candidate {
		return NewCandidate(index.Scratch{ID: id, Name: name, CreatedAt: base}, id+"  "+name)
	}
	return []Candidate{mk("aaa1", "todo"), mk("bbb2", "budget"), mk("ccc3", "notes")}
}

// drive builds an IO around a canned stdin string and captured out/err buffers.
func drive(input string) (IO, *bytes.Buffer, *bytes.Buffer) {
	var out, errb bytes.Buffer
	return IO{In: strings.NewReader(input), Out: &out, Err: &errb}, &out, &errb
}

func TestSelectEmptyCandidatesCancels(t *testing.T) {
	streams, _, _ := drive("")
	_, err := Select(streams, nil, Options{TTY: true, AllowFzf: false})
	if !errors.Is(err, ErrCanceled) {
		t.Errorf("selecting from an empty set should cancel; got %v", err)
	}
}

func TestNumberedPromptPicksByNumber(t *testing.T) {
	// Non-TTY path: one-shot numbered prompt. "2" selects the second candidate.
	streams, out, _ := drive("2\n")
	got, err := Select(streams, threeCands(), Options{TTY: false, AllowFzf: true})
	if err != nil {
		t.Fatalf("unexpected error: %v (out=%q)", err, out.String())
	}
	if got.Scratch.ID != "bbb2" {
		t.Errorf("chose wrong scratch; got %q want bbb2", got.Scratch.ID)
	}
	// fzf must never be consulted on a non-TTY, even when AllowFzf is true.
	if strings.Contains(out.String(), "fzf") {
		t.Error("non-TTY path should not mention fzf")
	}
}

func TestNumberedPromptOutOfRangeCancels(t *testing.T) {
	streams, _, _ := drive("9\n")
	_, err := Select(streams, threeCands(), Options{TTY: false})
	if !errors.Is(err, ErrCanceled) {
		t.Errorf("an out-of-range number should cancel on the numbered path; got %v", err)
	}
}

func TestNumberedPromptEmptyLineCancels(t *testing.T) {
	streams, _, _ := drive("\n")
	if _, err := Select(streams, threeCands(), Options{TTY: false}); !errors.Is(err, ErrCanceled) {
		t.Errorf("an empty line should cancel the numbered prompt; got %v", err)
	}
}

func TestInteractivePicksByNumber(t *testing.T) {
	// TTY, no fzf allowed → interactive loop. First line "3" picks the third.
	streams, out, _ := drive("3\n")
	got, err := Select(streams, threeCands(), Options{TTY: true, AllowFzf: false})
	if err != nil {
		t.Fatalf("unexpected error: %v (out=%q)", err, out.String())
	}
	if got.Scratch.ID != "ccc3" {
		t.Errorf("interactive pick wrong; got %q want ccc3", got.Scratch.ID)
	}
}

func TestInteractiveFiltersThenPicks(t *testing.T) {
	// Type a query that narrows to one, then accept with an empty line (which
	// takes the top of the filtered list).
	streams, out, _ := drive("budg\n\n")
	got, err := Select(streams, threeCands(), Options{TTY: true, AllowFzf: false})
	if err != nil {
		t.Fatalf("unexpected error: %v (out=%q)", err, out.String())
	}
	if got.Scratch.ID != "bbb2" {
		t.Errorf("filter+accept picked wrong; got %q want bbb2", got.Scratch.ID)
	}
}

func TestInteractiveEmptyLineAcceptsTop(t *testing.T) {
	// With no filter, an immediate empty line accepts the first candidate.
	streams, _, _ := drive("\n")
	got, err := Select(streams, threeCands(), Options{TTY: true, AllowFzf: false})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Scratch.ID != "aaa1" {
		t.Errorf("empty line should accept the top candidate; got %q", got.Scratch.ID)
	}
}

func TestInteractiveQCancels(t *testing.T) {
	streams, _, _ := drive("q\n")
	if _, err := Select(streams, threeCands(), Options{TTY: true, AllowFzf: false}); !errors.Is(err, ErrCanceled) {
		t.Errorf("q should cancel the interactive picker; got %v", err)
	}
}

func TestInteractiveEOFCancels(t *testing.T) {
	// Reader that yields no newline and then EOF: the loop should cancel, not spin.
	streams, _, _ := drive("")
	if _, err := Select(streams, threeCands(), Options{TTY: true, AllowFzf: false}); !errors.Is(err, ErrCanceled) {
		t.Errorf("EOF at the prompt should cancel; got %v", err)
	}
}

func TestInteractiveNoMatchThenValidQuery(t *testing.T) {
	// A query that matches nothing keeps the loop alive; a following good query
	// + empty-line accept should still succeed.
	streams, out, _ := drive("zzz\nnotes\n\n")
	got, err := Select(streams, threeCands(), Options{TTY: true, AllowFzf: false})
	if err != nil {
		t.Fatalf("unexpected error: %v (out=%q)", err, out.String())
	}
	if got.Scratch.ID != "ccc3" {
		t.Errorf("recovered pick wrong; got %q want ccc3", got.Scratch.ID)
	}
	if !strings.Contains(out.String(), "no scratch matches") {
		t.Error("a no-match query should print a hint before continuing")
	}
}

func TestSelectPrefersFzfWhenPresentAndAllowed(t *testing.T) {
	// Fake fzf: report it's installed, and have it return the label of the
	// second candidate. Select should map that back to bbb2.
	cands := threeCands()
	wantLabel := cands[1].Label
	opts := Options{
		TTY:      true,
		AllowFzf: true,
		lookFzf:  func() (string, bool) { return "/fake/fzf", true },
		runFzf: func(_ IO, labels []string) (string, error) {
			if len(labels) != len(cands) {
				t.Fatalf("fzf got %d labels, want %d", len(labels), len(cands))
			}
			return wantLabel + "\n", nil
		},
	}
	streams, _, _ := drive("")
	got, err := Select(streams, cands, opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Scratch.ID != "bbb2" {
		t.Errorf("fzf selection mapped wrong; got %q want bbb2", got.Scratch.ID)
	}
}

func TestSelectFzfNoSelectionCancels(t *testing.T) {
	opts := Options{
		TTY:      true,
		AllowFzf: true,
		lookFzf:  func() (string, bool) { return "/fake/fzf", true },
		runFzf:   func(_ IO, _ []string) (string, error) { return "", ErrCanceled },
	}
	streams, _, _ := drive("")
	if _, err := Select(streams, threeCands(), opts); !errors.Is(err, ErrCanceled) {
		t.Errorf("fzf cancel should propagate as ErrCanceled; got %v", err)
	}
}

func TestSelectNoFzfOptionSkipsFzf(t *testing.T) {
	// AllowFzf=false must not call lookFzf/runFzf even on a TTY; it should fall
	// through to the interactive loop. We prove it by failing the test if the
	// fzf fakes are ever invoked.
	opts := Options{
		TTY:      true,
		AllowFzf: false,
		lookFzf:  func() (string, bool) { t.Fatal("lookFzf called despite AllowFzf=false"); return "", false },
		runFzf: func(_ IO, _ []string) (string, error) {
			t.Fatal("runFzf called despite AllowFzf=false")
			return "", nil
		},
	}
	streams, _, _ := drive("1\n")
	got, err := Select(streams, threeCands(), opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Scratch.ID != "aaa1" {
		t.Errorf("expected interactive path to pick aaa1; got %q", got.Scratch.ID)
	}
}

func TestSelectFzfUnknownLabelCancels(t *testing.T) {
	// If fzf echoes back something we never offered, treat it as a cancel
	// rather than opening the wrong scratch.
	opts := Options{
		TTY:      true,
		AllowFzf: true,
		lookFzf:  func() (string, bool) { return "/fake/fzf", true },
		runFzf:   func(_ IO, _ []string) (string, error) { return "not-a-real-label\n", nil },
	}
	streams, _, _ := drive("")
	if _, err := Select(streams, threeCands(), opts); !errors.Is(err, ErrCanceled) {
		t.Errorf("an unrecognized fzf line should cancel; got %v", err)
	}
}
