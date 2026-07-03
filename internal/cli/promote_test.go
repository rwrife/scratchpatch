package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeScratchBody writes body into the live content file for id (ext txt, as
// created by newScratchID) so promote has real content to move.
func (s *session) writeScratchBody(id, body string) string {
	s.t.Helper()
	matches, _ := filepath.Glob(filepath.Join(s.home, "scratches", id+".txt"))
	if len(matches) != 1 {
		s.t.Fatalf("expected one scratch file for %s, got %v", id, matches)
	}
	if err := os.WriteFile(matches[0], []byte(body), 0o600); err != nil {
		s.t.Fatalf("write scratch body: %v", err)
	}
	return matches[0]
}

func TestPromoteMovesScratchIntoDestAndDropsIt(t *testing.T) {
	s := newSession(t)
	id := s.newScratchID("keep-this")
	s.writeScratchBody(id, "promote me\n")

	destDir := t.TempDir()
	dest := filepath.Join(destDir, "kept.md")

	out, err := s.run("promote", id, dest)
	if err != nil {
		t.Fatalf("promote: %v (out=%s)", err, out)
	}
	if !strings.Contains(out, "promoted scratch") || !strings.Contains(out, dest) {
		t.Errorf("promote should confirm the new path; got %q", out)
	}

	// The file is now in the repo with its content intact...
	body, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("read promoted file: %v", err)
	}
	if string(body) != "promote me\n" {
		t.Errorf("promoted content = %q, want %q", body, "promote me\n")
	}

	// ...and the store no longer lists it.
	lsOut, _ := s.run("ls")
	if strings.Contains(lsOut, "keep-this") {
		t.Errorf("promoted scratch should vanish from ls; got %q", lsOut)
	}
	// Its store file is gone too.
	if matches, _ := filepath.Glob(filepath.Join(s.home, "scratches", id+".*")); len(matches) != 0 {
		t.Errorf("store file should be gone after promote; got %v", matches)
	}
}

func TestPromoteIntoDirectoryUsesSluggedName(t *testing.T) {
	s := newSession(t)
	id := s.newScratchID("Deploy Notes")
	s.writeScratchBody(id, "steps\n")

	destDir := t.TempDir()
	out, err := s.run("promote", id, destDir)
	if err != nil {
		t.Fatalf("promote into dir: %v (out=%s)", err, out)
	}

	// Name is slugged, ext preserved from the scratch (txt in the harness).
	want := filepath.Join(destDir, "deploy-notes.txt")
	if _, err := os.Stat(want); err != nil {
		t.Errorf("expected promoted file at %s: %v (out=%s)", want, err, out)
	}
}

func TestPromoteDefaultsToCurrentDir(t *testing.T) {
	s := newSession(t)
	id := s.newScratchID("here-please")
	s.writeScratchBody(id, "local\n")

	// Run from a scratch working dir so a bare `sp promote <id>` lands here.
	workdir := t.TempDir()
	oldWd, _ := os.Getwd()
	if err := os.Chdir(workdir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWd) })

	if out, err := s.run("promote", id); err != nil {
		t.Fatalf("promote (default dir): %v (out=%s)", err, out)
	}
	if _, err := os.Stat(filepath.Join(workdir, "here-please.txt")); err != nil {
		t.Errorf("expected promoted file in cwd: %v", err)
	}
}

func TestPromoteRefusesOverwriteWithoutForce(t *testing.T) {
	s := newSession(t)
	id := s.newScratchID("nope")
	s.writeScratchBody(id, "new\n")

	dest := filepath.Join(t.TempDir(), "taken.md")
	if err := os.WriteFile(dest, []byte("keep me\n"), 0o600); err != nil {
		t.Fatalf("pre-write dest: %v", err)
	}

	_, err := s.run("promote", id, dest)
	if err == nil {
		t.Fatal("promote onto an existing file should error without --force")
	}
	if !strings.Contains(err.Error(), "--force") {
		t.Errorf("error should hint at --force; got %v", err)
	}

	// Non-destructive: destination untouched, scratch still present.
	body, _ := os.ReadFile(dest)
	if string(body) != "keep me\n" {
		t.Errorf("destination should be untouched; got %q", body)
	}
	lsOut, _ := s.run("ls")
	if !strings.Contains(lsOut, "nope") {
		t.Errorf("refused promote should leave the scratch listed; got %q", lsOut)
	}
}

func TestPromoteForceOverwrites(t *testing.T) {
	s := newSession(t)
	id := s.newScratchID("clobber")
	s.writeScratchBody(id, "fresh\n")

	dest := filepath.Join(t.TempDir(), "target.md")
	if err := os.WriteFile(dest, []byte("stale\n"), 0o600); err != nil {
		t.Fatalf("pre-write dest: %v", err)
	}

	if out, err := s.run("promote", id, dest, "--force"); err != nil {
		t.Fatalf("promote --force: %v (out=%s)", err, out)
	}
	body, _ := os.ReadFile(dest)
	if string(body) != "fresh\n" {
		t.Errorf("--force should overwrite; got %q", body)
	}
}

func TestPromoteUnknownIDErrors(t *testing.T) {
	s := newSession(t)
	_, err := s.run("promote", "ghost123")
	if err == nil {
		t.Fatal("promote of an unknown id should error")
	}
	if !strings.Contains(err.Error(), "no scratch matches") {
		t.Errorf("error should mention no match; got %v", err)
	}
}

func TestPromotePrefixResolution(t *testing.T) {
	s := newSession(t)
	id := s.newScratchID("prefixy")
	s.writeScratchBody(id, "body\n")

	destDir := t.TempDir()
	if out, err := s.run("promote", id[:4], destDir); err != nil {
		t.Fatalf("promote by prefix: %v (out=%s)", err, out)
	}
	if _, err := os.Stat(filepath.Join(destDir, "prefixy.txt")); err != nil {
		t.Errorf("prefix promote should land the file: %v", err)
	}
}
