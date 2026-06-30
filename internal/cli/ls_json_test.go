package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

// seedScratch creates one scratch in an isolated store and returns its id,
// reusing runCmd's env wiring. It keeps the JSON tests focused on output shape
// rather than setup.
func lsJSON(t *testing.T, home string, args ...string) string {
	t.Helper()
	t.Setenv("SCRATCHPATCH_HOME", home)
	t.Setenv("EDITOR", "")
	root := NewRootCommand()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(args)
	if err := root.Execute(); err != nil {
		t.Fatalf("%v: %v (out=%s)", args, err, out.String())
	}
	return out.String()
}

func TestLsJSONEmptyStoreIsEmptyArray(t *testing.T) {
	home := t.TempDir()
	got := lsJSON(t, home, "ls", "--json")
	if strings.TrimSpace(got) != "[]" {
		t.Errorf("empty `ls --json` should be []; got %q", got)
	}
	// Must be valid JSON and unmarshal to an empty slice (not null).
	var recs []map[string]any
	if err := json.Unmarshal([]byte(got), &recs); err != nil {
		t.Fatalf("output is not valid JSON: %v (%q)", err, got)
	}
	if recs == nil || len(recs) != 0 {
		t.Errorf("expected empty non-nil array, got %#v", recs)
	}
}

func TestLsJSONListsScratchesWithComputedFields(t *testing.T) {
	home := t.TempDir()
	// Seed two scratches; the second (beta) is newest and should sort first.
	lsJSON(t, home, "new", "alpha", "--no-edit", "--tag", "work", "--tag", "urgent")
	lsJSON(t, home, "new", "beta", "--no-edit", "--ext", "txt")

	got := lsJSON(t, home, "ls", "--json")

	// No color codes ever leak into JSON, even though it's the same data the
	// table would tint.
	if strings.Contains(got, "\x1b[") {
		t.Errorf("`ls --json` must be colorless; got %q", got)
	}

	var recs []struct {
		ID           string   `json:"id"`
		Name         string   `json:"name"`
		Tags         []string `json:"tags"`
		Ext          string   `json:"ext"`
		Status       string   `json:"status"`
		ExpiresHuman string   `json:"expiresHuman"`
		AgeHuman     string   `json:"ageHuman"`
	}
	if err := json.Unmarshal([]byte(got), &recs); err != nil {
		t.Fatalf("invalid JSON: %v (%q)", err, got)
	}
	if len(recs) != 2 {
		t.Fatalf("expected 2 records, got %d (%q)", len(recs), got)
	}

	// Newest-first ordering: beta before alpha.
	if recs[0].Name != "beta" || recs[1].Name != "alpha" {
		t.Errorf("expected newest-first [beta, alpha]; got [%s, %s]", recs[0].Name, recs[1].Name)
	}
	if recs[0].Ext != "txt" {
		t.Errorf("beta ext = %q, want txt", recs[0].Ext)
	}
	// A freshly created scratch with a long default TTL is "fresh".
	if recs[1].Status != "fresh" {
		t.Errorf("alpha status = %q, want fresh", recs[1].Status)
	}
	// Tags round-trip; alpha has both, beta has an empty (non-null) slice.
	if len(recs[1].Tags) != 2 || recs[1].Tags[0] != "work" || recs[1].Tags[1] != "urgent" {
		t.Errorf("alpha tags = %#v, want [work urgent]", recs[1].Tags)
	}
	if recs[0].Tags == nil {
		t.Errorf("beta tags should be an empty array, not null")
	}
	// Computed human fields are populated.
	if recs[0].ExpiresHuman == "" || recs[0].AgeHuman == "" {
		t.Errorf("expected populated human fields, got %#v", recs[0])
	}
}

func TestLsMorgueJSON(t *testing.T) {
	home := t.TempDir()
	lsJSON(t, home, "new", "doomed", "--no-edit")

	// Find the id, soft-delete it, then list the morgue as JSON.
	live := lsJSON(t, home, "ls", "--json")
	var liveRecs []struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(live), &liveRecs); err != nil {
		t.Fatalf("invalid live JSON: %v", err)
	}
	if len(liveRecs) != 1 {
		t.Fatalf("expected 1 live scratch, got %d", len(liveRecs))
	}
	lsJSON(t, home, "rm", liveRecs[0].ID)

	got := lsJSON(t, home, "ls", "--morgue", "--json")
	var dead []struct {
		ID         string `json:"id"`
		Name       string `json:"name"`
		PurgeHuman string `json:"purgeHuman"`
		Purgeable  bool   `json:"purgeable"`
	}
	if err := json.Unmarshal([]byte(got), &dead); err != nil {
		t.Fatalf("invalid morgue JSON: %v (%q)", err, got)
	}
	if len(dead) != 1 {
		t.Fatalf("expected 1 morgue record, got %d (%q)", len(dead), got)
	}
	if dead[0].Name != "doomed" {
		t.Errorf("morgue record name = %q, want doomed", dead[0].Name)
	}
	// Freshly deleted with a multi-day grace window: not yet purgeable.
	if dead[0].Purgeable {
		t.Errorf("a just-deleted scratch should not be purgeable yet: %#v", dead[0])
	}
	if dead[0].PurgeHuman == "" {
		t.Errorf("expected a populated purgeHuman, got empty")
	}
}

func TestLsMorgueJSONEmptyIsArray(t *testing.T) {
	home := t.TempDir()
	got := lsJSON(t, home, "ls", "--morgue", "--json")
	if strings.TrimSpace(got) != "[]" {
		t.Errorf("empty morgue --json should be []; got %q", got)
	}
}
