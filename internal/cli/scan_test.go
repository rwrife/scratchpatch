package cli

import (
	"errors"
	"strings"
	"testing"
)

// dotenvBody is a small .env-style dump with real-looking secrets used across
// the scan/promote tripwire tests.
const dotenvBody = "# creds\n" +
	"AWS_ACCESS_KEY_ID=AKIAIOSFODNN7EXAMPLE\n" +
	"STRIPE_API_KEY=EXAMPLExQzLkdIwqZ9mNbVcXsWePqRtY\n" +
	"NOTES=this is just prose and should stay quiet\n"

func TestScanCleanScratchExitsZeroAndSaysSoClean(t *testing.T) {
	s := newSession(t)
	id := s.newScratchID("safe-notes")
	s.writeScratchBody(id, "just some harmless notes\nnothing to see here\n")

	out, err := s.run("scan", id)
	if err != nil {
		t.Fatalf("scan of a clean scratch should exit zero; got %v (out=%s)", err, out)
	}
	if !strings.Contains(strings.ToLower(out), "clean") {
		t.Errorf("scan should report a clean bill of health; got %q", out)
	}
}

func TestScanTrippedScratchReportsMaskedFindingsAndErrors(t *testing.T) {
	s := newSession(t)
	id := s.newScratchID("leaky")
	s.writeScratchBody(id, dotenvBody)

	out, err := s.run("scan", id)
	if err == nil {
		t.Fatal("scan of a scratch with secrets should return the secrets-found sentinel")
	}
	if !errors.Is(err, ErrSecretsFound) {
		t.Errorf("expected ErrSecretsFound sentinel; got %v", err)
	}

	// The report must name findings by line but never echo a raw secret.
	if !strings.Contains(out, "tripwire") {
		t.Errorf("scan should headline the tripwire hit; got %q", out)
	}
	for _, raw := range []string{"AKIAIOSFODNN7EXAMPLE", "EXAMPLExQzLkdIwqZ9mNbVcXsWePqRtY"} {
		if strings.Contains(out, raw) {
			t.Errorf("scan output leaked a raw secret %q: %s", raw, out)
		}
	}
	// Masked previews should be present (first-3…last-3 form).
	if !strings.Contains(out, "AKI") || !strings.Contains(out, "…") {
		t.Errorf("scan should show masked previews; got %q", out)
	}
}

func TestScanJSONShapeIsStable(t *testing.T) {
	s := newSession(t)
	id := s.newScratchID("leaky-json")
	s.writeScratchBody(id, dotenvBody)

	out, _ := s.run("scan", id, "--json")
	// --json must be pure data: no personality words, a tripped flag, and a
	// findings array. (We assert on substrings rather than parsing to keep the
	// test close to the other CLI tests in this package.)
	for _, want := range []string{"\"tripped\": true", "\"findings\"", "\"kind\"", "\"line\"", "\"masked\""} {
		if !strings.Contains(out, want) {
			t.Errorf("scan --json missing %q; got %s", want, out)
		}
	}
	if strings.Contains(out, "tripwire") || strings.Contains(out, "🔑") {
		t.Errorf("scan --json should carry no personality/markers; got %s", out)
	}
}

func TestScanPrefixAndUnknownID(t *testing.T) {
	s := newSession(t)
	id := s.newScratchID("prefixy-scan")
	s.writeScratchBody(id, "clean\n")

	if _, err := s.run("scan", id[:4]); err != nil {
		t.Errorf("scan by prefix should work on a clean scratch; got %v", err)
	}

	_, err := s.run("scan", "ghost999")
	if err == nil || !strings.Contains(err.Error(), "no scratch matches") {
		t.Errorf("scan of unknown id should error with no-match; got %v", err)
	}
}
