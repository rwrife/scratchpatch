package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// newScratchIDExt creates a named scratch with an explicit extension and digs
// its id back out by globbing for that ext, so tests that need two coexisting
// scratches can tell them apart (the default newScratchID globs *.txt and is
// only reliable for one txt scratch at a time).
func (s *session) newScratchIDExt(name, ext string) string {
	s.t.Helper()
	if out, err := s.run("new", name, "--no-edit", "--ext", ext); err != nil {
		s.t.Fatalf("new %q .%s: %v (out=%s)", name, ext, err, out)
	}
	matches, _ := filepath.Glob(filepath.Join(s.home, "scratches", "*."+ext))
	if len(matches) == 0 {
		s.t.Fatalf("no .%s scratch created for %q", ext, name)
	}
	base := filepath.Base(matches[len(matches)-1])
	return strings.TrimSuffix(base, "."+ext)
}

// writeScratchBodyExt writes body into the live content file for id with the
// given ext.
func (s *session) writeScratchBodyExt(id, ext, body string) string {
	s.t.Helper()
	path := filepath.Join(s.home, "scratches", id+"."+ext)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		s.t.Fatalf("write scratch body: %v", err)
	}
	return path
}

func TestPromoteBlocksScratchWithSecrets(t *testing.T) {
	s := newSession(t)
	id := s.newScratchID("leaky-promote")
	s.writeScratchBody(id, dotenvBody)

	dest := filepath.Join(t.TempDir(), "escaped.env")
	_, err := s.run("promote", id, dest)
	if err == nil {
		t.Fatal("promote should refuse a scratch that trips the secret tripwire")
	}
	if !strings.Contains(err.Error(), "--allow-secrets") {
		t.Errorf("block should point at --allow-secrets; got %v", err)
	}
	// The block must not echo the secret value.
	if strings.Contains(err.Error(), "AKIAIOSFODNN7EXAMPLE") {
		t.Errorf("promote block leaked a raw secret: %v", err)
	}

	// Non-destructive: the file did not escape and the scratch is still listed.
	if _, statErr := os.Stat(dest); statErr == nil {
		t.Errorf("blocked promote must not create the destination file")
	}
	lsOut, _ := s.run("ls")
	if !strings.Contains(lsOut, "leaky-promote") {
		t.Errorf("blocked scratch should remain listed; got %q", lsOut)
	}
}

func TestPromoteAllowSecretsOverridesTheBlock(t *testing.T) {
	s := newSession(t)
	id := s.newScratchID("override-me")
	s.writeScratchBody(id, dotenvBody)

	dest := filepath.Join(t.TempDir(), "kept.env")
	out, err := s.run("promote", id, dest, "--allow-secrets")
	if err != nil {
		t.Fatalf("promote --allow-secrets should succeed; got %v (out=%s)", err, out)
	}
	if _, statErr := os.Stat(dest); statErr != nil {
		t.Errorf("promote --allow-secrets should move the file: %v", statErr)
	}
	// And the scratch is gone from the store, like any promote.
	lsOut, _ := s.run("ls")
	if strings.Contains(lsOut, "override-me") {
		t.Errorf("promoted scratch should vanish from ls; got %q", lsOut)
	}
}

func TestPromoteCleanScratchStillWorks(t *testing.T) {
	// Regression guard: the tripwire must not interfere with normal promotes.
	s := newSession(t)
	id := s.newScratchID("totally-clean")
	s.writeScratchBody(id, "no secrets here, just notes\n")

	dest := filepath.Join(t.TempDir(), "notes.md")
	if out, err := s.run("promote", id, dest); err != nil {
		t.Fatalf("clean promote should succeed; got %v (out=%s)", err, out)
	}
}

func TestLsMarksScratchesThatTripTheTripwire(t *testing.T) {
	s := newSession(t)
	// Distinct exts so newScratchID resolves each id unambiguously (the harness
	// globs by ext, so two same-ext scratches would collide).
	leakyID := s.newScratchIDExt("leaky-ls", "env")
	s.writeScratchBodyExt(leakyID, "env", dotenvBody)
	cleanID := s.newScratchIDExt("clean-ls", "md")
	s.writeScratchBodyExt(cleanID, "md", "harmless\n")

	out, err := s.run("ls")
	if err != nil {
		t.Fatalf("ls: %v", err)
	}
	// The leaky scratch's line carries the 🔑 marker; the clean one does not.
	// Match on id so the two rows are told apart unambiguously.
	var sawLeaky, sawClean bool
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, leakyID) {
			sawLeaky = true
			if !strings.Contains(line, "🔑") {
				t.Errorf("leaky scratch line should carry the 🔑 marker; got %q", line)
			}
		}
		if strings.Contains(line, cleanID) {
			sawClean = true
			if strings.Contains(line, "🔑") {
				t.Errorf("clean scratch line should not be marked; got %q", line)
			}
		}
	}
	if !sawLeaky || !sawClean {
		t.Fatalf("expected both scratches listed; sawLeaky=%v sawClean=%v out=%q", sawLeaky, sawClean, out)
	}
}

func TestLsJSONCarriesSecretFlag(t *testing.T) {
	s := newSession(t)
	leakyID := s.newScratchID("leaky-json-ls")
	s.writeScratchBody(leakyID, dotenvBody)

	out, err := s.run("ls", "--json")
	if err != nil {
		t.Fatalf("ls --json: %v", err)
	}
	if !strings.Contains(out, "\"secret\": true") {
		t.Errorf("ls --json should mark the leaky scratch with secret=true; got %s", out)
	}
	// JSON stays personality-free — no marker glyph in the machine output.
	if strings.Contains(out, "🔑") {
		t.Errorf("ls --json must not contain the marker glyph; got %s", out)
	}
}
