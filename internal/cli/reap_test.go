package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// indexFile mirrors the on-disk shape so reap tests can age scratches (move
// ExpiresAt/DeletedAt into the past) without exporting test hooks from the
// store. It only needs the fields the reaper reads.
type tIndexFile struct {
	Schema    int        `json:"schema"`
	Scratches []tScratch `json:"scratches"`
}

type tScratch struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	CreatedAt time.Time  `json:"createdAt"`
	TTL       string     `json:"ttl"`
	ExpiresAt time.Time  `json:"expiresAt"`
	Tags      []string   `json:"tags,omitempty"`
	Ext       string     `json:"ext"`
	OriginCwd string     `json:"originCwd"`
	Size      int64      `json:"size"`
	DeletedAt *time.Time `json:"deletedAt,omitempty"`
}

func (s *session) loadIndex() tIndexFile {
	s.t.Helper()
	b, err := os.ReadFile(filepath.Join(s.home, "index.json"))
	if err != nil {
		s.t.Fatalf("read index: %v", err)
	}
	var f tIndexFile
	if err := json.Unmarshal(b, &f); err != nil {
		s.t.Fatalf("parse index: %v", err)
	}
	return f
}

func (s *session) saveIndex(f tIndexFile) {
	s.t.Helper()
	b, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		s.t.Fatalf("encode index: %v", err)
	}
	if err := os.WriteFile(filepath.Join(s.home, "index.json"), b, 0o600); err != nil {
		s.t.Fatalf("write index: %v", err)
	}
}

// expire forces the named scratch's ExpiresAt into the past so reap's stage 1
// treats it as expired.
func (s *session) expire(id string) {
	s.t.Helper()
	f := s.loadIndex()
	found := false
	for i := range f.Scratches {
		if f.Scratches[i].ID == id {
			f.Scratches[i].ExpiresAt = time.Now().Add(-time.Hour)
			found = true
		}
	}
	if !found {
		s.t.Fatalf("expire: scratch %s not in index", id)
	}
	s.saveIndex(f)
}

// agePastGrace forces the named (already-morgued) scratch's DeletedAt far enough
// into the past that its purge deadline has elapsed.
func (s *session) agePastGrace(id string) {
	s.t.Helper()
	f := s.loadIndex()
	old := time.Now().Add(-30 * 24 * time.Hour)
	found := false
	for i := range f.Scratches {
		if f.Scratches[i].ID == id {
			f.Scratches[i].DeletedAt = &old
			found = true
		}
	}
	if !found {
		s.t.Fatalf("agePastGrace: scratch %s not in index", id)
	}
	s.saveIndex(f)
}

func (s *session) indexHas(id string) bool {
	s.t.Helper()
	for _, sc := range s.loadIndex().Scratches {
		if sc.ID == id {
			return true
		}
	}
	return false
}

func TestReapEmptyStore(t *testing.T) {
	s := newSession(t)
	out, err := s.run("reap")
	if err != nil {
		t.Fatalf("reap: %v (out=%s)", err, out)
	}
	if !strings.Contains(out, "nothing to reap") {
		t.Errorf("empty reap should say nothing to reap, got: %q", out)
	}
}

func TestReapSweepsExpiredLiveToMorgue(t *testing.T) {
	s := newSession(t)
	id := s.newScratchID("doomed")
	s.expire(id)

	out, err := s.run("reap")
	if err != nil {
		t.Fatalf("reap: %v (out=%s)", err, out)
	}
	if !strings.Contains(out, id) {
		t.Errorf("reap output should mention swept scratch %s, got: %q", id, out)
	}

	// It should now be in the morgue, not live.
	live, _ := s.run("ls")
	if strings.Contains(live, id) {
		t.Errorf("swept scratch %s should no longer be live: %q", id, live)
	}
	morgue, _ := s.run("ls", "--morgue")
	if !strings.Contains(morgue, id) {
		t.Errorf("swept scratch %s should be in the morgue: %q", id, morgue)
	}
	// And it must NOT have been purged from the index in the same pass.
	if !s.indexHas(id) {
		t.Error("a freshly-swept scratch must survive in the index (grace starts now)")
	}
}

func TestReapPurgesPastGraceMorgueItem(t *testing.T) {
	s := newSession(t)
	id := s.newScratchID("ancient")
	// Move it to the morgue, then age it past grace.
	if out, err := s.run("rm", id); err != nil {
		t.Fatalf("rm: %v (out=%s)", err, out)
	}
	s.agePastGrace(id)

	morgueBefore := filepath.Join(s.home, "morgue", id+".txt")
	if _, err := os.Stat(morgueBefore); err != nil {
		t.Fatalf("expected morgue content before reap: %v", err)
	}

	out, err := s.run("reap")
	if err != nil {
		t.Fatalf("reap: %v (out=%s)", err, out)
	}
	if !strings.Contains(out, "for good") {
		t.Errorf("reap headline should mention purging for good, got: %q", out)
	}

	// Gone from disk and index.
	if _, err := os.Stat(morgueBefore); !os.IsNotExist(err) {
		t.Errorf("purged content should be gone, stat err = %v", err)
	}
	if s.indexHas(id) {
		t.Error("purged scratch should be gone from the index")
	}
}

func TestReapDryRunChangesNothing(t *testing.T) {
	s := newSession(t)
	id := s.newScratchID("preview")
	s.expire(id)

	out, err := s.run("reap", "--dry-run")
	if err != nil {
		t.Fatalf("reap --dry-run: %v (out=%s)", err, out)
	}
	if !strings.Contains(out, "dry run") {
		t.Errorf("dry-run output should announce itself, got: %q", out)
	}
	if !strings.Contains(out, id) {
		t.Errorf("dry-run should still list the would-be-swept scratch %s, got: %q", id, out)
	}

	// Nothing actually moved: it's still live.
	live, _ := s.run("ls")
	if !strings.Contains(live, id) {
		t.Errorf("dry-run must leave scratch %s live, got: %q", id, live)
	}
}

func TestNewTTLAcceptsHumanDurations(t *testing.T) {
	s := newSession(t)
	if out, err := s.run("new", "weekish", "--no-edit", "--ext", "txt", "--ttl", "2w"); err != nil {
		t.Fatalf("new --ttl 2w: %v (out=%s)", err, out)
	}
	f := s.loadIndex()
	if len(f.Scratches) != 1 {
		t.Fatalf("want 1 scratch, got %d", len(f.Scratches))
	}
	// 2 weeks = 336h; the on-disk TTL is a Go duration string.
	if f.Scratches[0].TTL != "336h0m0s" {
		t.Errorf("TTL = %q, want 336h0m0s", f.Scratches[0].TTL)
	}
	// ExpiresAt should be ~2w out from CreatedAt.
	got := f.Scratches[0].ExpiresAt.Sub(f.Scratches[0].CreatedAt)
	if got != 14*24*time.Hour {
		t.Errorf("ExpiresAt-CreatedAt = %v, want 336h", got)
	}
}

func TestNewTTLRejectsGarbage(t *testing.T) {
	s := newSession(t)
	out, err := s.run("new", "bad", "--no-edit", "--ttl", "soon")
	if err == nil {
		t.Fatalf("new --ttl soon should fail, got out=%q", out)
	}
	// No scratch should have been created on a bad ttl.
	matches, _ := filepath.Glob(filepath.Join(s.home, "scratches", "*"))
	if len(matches) != 0 {
		t.Errorf("a rejected --ttl must not create a scratch, found %v", matches)
	}
}
