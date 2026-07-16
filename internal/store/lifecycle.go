// M4 lifecycle: the soft-delete safety net.
//
// This file adds the move operations that make scratchpatch's core promise —
// "nothing is ever lost in one step" — real. `rm` moves a scratch's content
// from scratches/ to morgue/ and stamps DeletedAt; `resurrect` moves it back
// and clears the stamp. The index and the filesystem are kept in lockstep here
// so callers never see a half-moved scratch.
//
// There is deliberately no hard-delete in this file. Purging content for good
// is M5's `reap`, and only for morgue items already past the grace window.
package store

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/rwrife/scratchpatch/internal/index"
)

// ErrAmbiguousID is returned when an id prefix matches more than one scratch.
var ErrAmbiguousID = errors.New("ambiguous id prefix")

// ErrDestinationExists is returned by Promote when the target path already
// exists and the caller didn't ask to overwrite it.
var ErrDestinationExists = errors.New("destination already exists")

// SetPin sets or clears the pinned flag on a scratch and persists the index,
// returning the updated record. Pinning is metadata-only: it never touches
// content or moves files, so it works on live and morgued scratches alike
// (pinning a morgued scratch is harmless — reap only consults the flag on the
// live set). Setting the flag to the value it already holds is a no-op write,
// which keeps `sp pin` idempotent. The scratch is re-fetched by id rather than
// trusting the passed-in copy so a stale caller can't clobber other fields.
func (s *Store) SetPin(id string, pinned bool) (index.Scratch, error) {
	sc, err := s.idx.Get(id)
	if err != nil {
		return index.Scratch{}, err
	}
	sc.Pinned = pinned
	if err := s.idx.Put(sc); err != nil {
		return index.Scratch{}, err
	}
	return sc, nil
}

// morguePath is the on-disk location for a soft-deleted scratch's content:
// id.ext under morgue/. It mirrors contentPath so a move is a same-name rename
// across the two directories.
func (s *Store) morguePath(sc index.Scratch) string {
	name := sc.ID
	if sc.Ext != "" {
		name += "." + sc.Ext
	}
	return filepath.Join(s.cfg.MorguePath(), name)
}

// LivePath returns where a scratch's content lives right now: morgue/ if it's
// been soft-deleted, scratches/ otherwise. Commands that read or open a scratch
// (cat/open) use this so they work whether or not the scratch is morgued.
func (s *Store) LivePath(sc index.Scratch) string {
	if sc.Morgued() {
		return s.morguePath(sc)
	}
	return s.contentPath(sc)
}

// Resolve looks up a scratch by an exact id or an unambiguous id prefix, so
// users don't have to type the full 8-char id. An exact match always wins; a
// prefix that matches exactly one scratch resolves to it; a prefix matching
// several returns ErrAmbiguousID; none returns index.ErrNotFound.
func (s *Store) Resolve(ref string) (index.Scratch, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return index.Scratch{}, fmt.Errorf("%q: %w", ref, index.ErrNotFound)
	}

	// Fast path: an exact id hit is unambiguous by construction.
	if sc, err := s.idx.Get(ref); err == nil {
		return sc, nil
	} else if !errors.Is(err, index.ErrNotFound) {
		return index.Scratch{}, err
	}

	all, err := s.idx.List()
	if err != nil {
		return index.Scratch{}, err
	}

	var matches []index.Scratch
	for _, sc := range all {
		if strings.HasPrefix(sc.ID, ref) {
			matches = append(matches, sc)
		}
	}
	switch len(matches) {
	case 0:
		return index.Scratch{}, fmt.Errorf("%q: %w", ref, index.ErrNotFound)
	case 1:
		return matches[0], nil
	default:
		ids := make([]string, len(matches))
		for i, m := range matches {
			ids[i] = m.ID
		}
		return index.Scratch{}, fmt.Errorf("%q matches %d scratches (%s): %w",
			ref, len(matches), strings.Join(ids, ", "), ErrAmbiguousID)
	}
}

// ListLive returns the live (non-morgued) scratches, newest-first.
func (s *Store) ListLive() ([]index.Scratch, error) {
	return s.listFiltered(func(sc index.Scratch) bool { return sc.Live() })
}

// ListMorgue returns the soft-deleted scratches, newest-first.
func (s *Store) ListMorgue() ([]index.Scratch, error) {
	return s.listFiltered(func(sc index.Scratch) bool { return sc.Morgued() })
}

func (s *Store) listFiltered(keep func(index.Scratch) bool) ([]index.Scratch, error) {
	all, err := s.idx.List()
	if err != nil {
		return nil, err
	}
	out := make([]index.Scratch, 0, len(all))
	for _, sc := range all {
		if keep(sc) {
			out = append(out, sc)
		}
	}
	return out, nil
}

// MoveToMorgue soft-deletes a scratch: it moves the content file from
// scratches/ to morgue/ and stamps DeletedAt in the index. It is a no-op error
// to morgue something already in the morgue. The filesystem move happens first;
// only if that succeeds is the index updated, and the move is rolled back if
// the index write fails — so the two never diverge.
func (s *Store) MoveToMorgue(sc index.Scratch) (index.Scratch, error) {
	if sc.Morgued() {
		return index.Scratch{}, fmt.Errorf("scratch %s is already in the morgue", sc.ID)
	}

	from := s.contentPath(sc)
	to := s.morguePath(sc)
	if err := moveFile(from, to); err != nil {
		return index.Scratch{}, fmt.Errorf("move scratch to morgue: %w", err)
	}

	now := time.Now()
	sc.DeletedAt = &now
	if err := s.idx.Put(sc); err != nil {
		// Roll the content back to live so the store stays consistent.
		_ = moveFile(to, from)
		return index.Scratch{}, fmt.Errorf("record soft-delete: %w", err)
	}
	return sc, nil
}

// Resurrect restores a soft-deleted scratch: it moves the content back from
// morgue/ to scratches/ and clears DeletedAt. Resurrecting a live scratch is an
// error. Like MoveToMorgue, the index is only updated after a successful move,
// and the move is rolled back if the index write fails.
func (s *Store) Resurrect(sc index.Scratch) (index.Scratch, error) {
	if !sc.Morgued() {
		return index.Scratch{}, fmt.Errorf("scratch %s is not in the morgue", sc.ID)
	}

	from := s.morguePath(sc)
	to := s.contentPath(sc)
	if err := moveFile(from, to); err != nil {
		return index.Scratch{}, fmt.Errorf("restore scratch from morgue: %w", err)
	}

	sc.DeletedAt = nil
	if err := s.idx.Put(sc); err != nil {
		_ = moveFile(to, from)
		return index.Scratch{}, fmt.Errorf("record resurrect: %w", err)
	}
	return sc, nil
}

// Promote graduates a scratch out of the store and into the wider filesystem:
// it moves the content file from wherever it lives (scratches/ or morgue/) to
// dst, then drops the scratch's index entry so the store no longer tracks it —
// the promoted file is the destination's responsibility now.
//
// dst must be the final absolute (or caller-resolved) target file path; Promote
// does not interpret directories or invent filenames, keeping this method a
// thin, testable move+forget. It refuses to clobber an existing dst unless
// overwrite is true. The filesystem move happens first; the index entry is only
// removed after the content has safely landed, and the move is rolled back if
// the index write fails — so a scratch is never lost between the two steps.
func (s *Store) Promote(sc index.Scratch, dst string, overwrite bool) error {
	if !overwrite {
		if _, err := os.Lstat(dst); err == nil {
			return fmt.Errorf("%w: %s", ErrDestinationExists, dst)
		} else if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("stat destination %s: %w", dst, err)
		}
	}

	// Make sure the destination directory exists so promoting into a nested
	// path (e.g. notes/keep.md) works without the caller pre-creating it.
	if dir := filepath.Dir(dst); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create destination dir %s: %w", dir, err)
		}
	}

	from := s.LivePath(sc)
	if err := moveFile(from, dst); err != nil {
		return fmt.Errorf("move scratch out of the store: %w", err)
	}

	if err := s.idx.Delete(sc.ID); err != nil {
		// Roll the content back to where it came from so the store stays
		// consistent if we couldn't forget the metadata.
		_ = moveFile(dst, from)
		return fmt.Errorf("drop promoted scratch from index: %w", err)
	}
	return nil
}

// PurgeAt returns when a morgued scratch becomes eligible for hard-deletion:
// DeletedAt + the configured grace window. The bool is false for live scratches
// (which have no purge deadline). M5's reap consumes this; ls --morgue renders
// the remaining time.
func (s *Store) PurgeAt(sc index.Scratch) (time.Time, bool) {
	if sc.DeletedAt == nil {
		return time.Time{}, false
	}
	return sc.DeletedAt.Add(s.cfg.Grace), true
}

// ReadContent returns the bytes of a scratch's content file, reading from
// whichever directory currently holds it (live or morgue). A missing file
// surfaces as an error so `sp cat` can report an orphaned index entry rather
// than silently printing nothing.
func (s *Store) ReadContent(sc index.Scratch) ([]byte, error) {
	path := s.LivePath(sc)
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read scratch %s content: %w", sc.ID, err)
	}
	return b, nil
}

// WriteContent overwrites a live scratch's content file with data and refreshes
// the recorded size in the index. It writes to the live path (scratches/) and
// is used by headless capture (`sp new --stdin`) to seed content without an
// editor round-trip. The returned Scratch carries the updated size.
func (s *Store) WriteContent(sc index.Scratch, data []byte) (index.Scratch, error) {
	path := s.LivePath(sc)
	if err := os.WriteFile(path, data, filePerm); err != nil {
		return index.Scratch{}, fmt.Errorf("write scratch %s content: %w", sc.ID, err)
	}
	return s.Touch(sc.ID)
}

// moveFile relocates src to dst. It tries an atomic rename first (the common
// case: same filesystem) and falls back to a copy+remove when rename fails with
// a cross-device error, so the store still works if scratches/ and morgue/ ever
// straddle a mount boundary.
func moveFile(src, dst string) error {
	if err := os.Rename(src, dst); err == nil {
		return nil
	} else if !isCrossDevice(err) {
		return err
	}
	return copyThenRemove(src, dst)
}

// isCrossDevice reports whether err is the EXDEV "invalid cross-device link"
// error that rename returns when src and dst live on different filesystems.
func isCrossDevice(err error) bool {
	var le *os.LinkError
	if errors.As(err, &le) {
		return errors.Is(le.Err, syscall.EXDEV)
	}
	return false
}

// copyThenRemove implements a non-atomic move: copy src to a temp file beside
// dst, fsync, rename into place, then remove src. The temp+rename keeps dst
// from ever appearing half-written.
func copyThenRemove(src, dst string) (err error) {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	info, err := in.Stat()
	if err != nil {
		return err
	}

	tmp, err := os.CreateTemp(filepath.Dir(dst), ".move-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() {
		if err != nil {
			_ = os.Remove(tmpName)
		}
	}()

	if _, err = io.Copy(tmp, in); err != nil {
		_ = tmp.Close()
		return err
	}
	if err = tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	if err = os.Chmod(tmpName, info.Mode().Perm()); err != nil {
		return err
	}
	if err = os.Rename(tmpName, dst); err != nil {
		return err
	}
	return os.Remove(src)
}
