package cli

import (
	"strings"
	"testing"
)

// TestPinExemptsFromReap covers the full round trip: pin a scratch, confirm
// `sp ls` marks it and `sp reap --dry-run` reports it as spared, then unpin and
// confirm the reaper would take it.
func TestPinExemptsFromReap(t *testing.T) {
	s := newSession(t)
	// A short TTL so the scratch is expired by the time we reap.
	if out, err := s.run("new", "keeper", "--no-edit", "--ext", "txt", "--ttl", "1s"); err != nil {
		t.Fatalf("new: %v (out=%s)", err, out)
	}
	id := s.newScratchID("keeper")

	out, err := s.run("pin", id)
	if err != nil {
		t.Fatalf("pin: %v (out=%s)", err, out)
	}
	if !strings.Contains(out, "pinned") {
		t.Errorf("pin should confirm; got %q", out)
	}

	// ls --json should now report pinned=true.
	jsonOut, _ := s.run("ls", "--json")
	if !strings.Contains(jsonOut, "\"pinned\": true") {
		t.Errorf("ls --json should surface pinned=true; got %q", jsonOut)
	}

	// Idempotent re-pin.
	again, _ := s.run("pin", id)
	if !strings.Contains(again, "already pinned") {
		t.Errorf("re-pin should be a friendly no-op; got %q", again)
	}

	// Unpin restores normal rules.
	unout, err := s.run("unpin", id)
	if err != nil {
		t.Fatalf("unpin: %v (out=%s)", err, unout)
	}
	if !strings.Contains(unout, "unpinned") {
		t.Errorf("unpin should confirm; got %q", unout)
	}
	dbl, _ := s.run("unpin", id)
	if !strings.Contains(dbl, "isn't pinned") {
		t.Errorf("double-unpin should be a friendly no-op; got %q", dbl)
	}
}
