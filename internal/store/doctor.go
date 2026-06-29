// M6 health check: reconciling the index against the filesystem.
//
// Everywhere else in scratchpatch the index and the on-disk content are kept in
// lockstep by construction — every move writes the filesystem first and rolls
// back if the index write fails, and vice versa. doctor is the audit that
// proves it (or catches the cases that construction can't: a crash between the
// two writes, a user reaching into the store by hand, a stale index copied from
// another machine).
//
// It is read-only. doctor never moves, deletes, or rewrites anything; it walks
// both sides and reports the discrepancies. Acting on them is left to the
// existing safe operations (`resurrect`, `reap`) and to the user's judgment —
// consistent with the "destructive actions are always two-step, never
// automatic" rule. Living in the store package keeps it the single place that
// reads the scratch directories, so no other layer has to learn the on-disk
// layout.
package store

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/rwrife/scratchpatch/internal/index"
)

// Orphan is a content file on disk with no matching index entry: scratchpatch
// is holding bytes it has forgotten how to describe. The path is recorded so a
// human can inspect (or remove) it; Area says which directory it turned up in.
type Orphan struct {
	// Path is the absolute path to the orphaned content file.
	Path string
	// Area is the human label for where it was found ("scratches" or
	// "morgue"), so the report can group without re-deriving it.
	Area string
	// Size is the file's size in bytes, so the report can show what's being
	// wasted and the store-size total can include orphans.
	Size int64
}

// Missing is the mirror image of an Orphan: an index entry whose content file
// is gone. The scratch is still listed by `sp ls`, but `sp cat`/`sp open` would
// fail — doctor surfaces it so it can be cleaned up deliberately rather than
// discovered by accident.
type Missing struct {
	// Scratch is the index record whose content is absent.
	Scratch index.Scratch
	// ExpectedPath is where the content should have been (live or morgue path
	// depending on the record's state), to aid debugging.
	ExpectedPath string
}

// Diagnosis is the full read-only health report doctor produces. It is plain
// data; the render layer decides how to phrase and color it.
type Diagnosis struct {
	// LiveCount and MorgueCount are how many index records are in each set,
	// for an at-a-glance "is the store sane" line.
	LiveCount   int
	MorgueCount int

	// Orphans are content files with no index entry, sorted by path.
	Orphans []Orphan

	// Missing are index entries with no content file, sorted by id.
	Missing []Missing

	// TrackedSize is the total size of content files that DO have an index
	// entry (the "real" store footprint). OrphanSize is the bytes held by
	// orphaned files. Kept separate so the report can show both the legitimate
	// footprint and the recoverable waste.
	TrackedSize int64
	OrphanSize  int64
}

// Healthy reports whether the store is fully consistent: no orphaned files and
// no missing content. A healthy diagnosis still carries the counts and sizes so
// `sp doctor` can print a reassuring summary rather than nothing.
func (d Diagnosis) Healthy() bool {
	return len(d.Orphans) == 0 && len(d.Missing) == 0
}

// TotalSize is the whole on-disk footprint of scratch content: tracked plus
// orphaned bytes. (Index file size is excluded; this is about scratch content.)
func (d Diagnosis) TotalSize() int64 {
	return d.TrackedSize + d.OrphanSize
}

// Doctor walks the index and the scratch directories and returns a Diagnosis of
// any drift between them. It is read-only and never changes the store.
//
// The reconciliation is set-based: build the set of content paths the index
// expects (each record's live-or-morgue path), then walk scratches/ and
// morgue/ on disk. A disk file the index expected is "tracked" (and its size
// counted); a disk file nothing expected is an Orphan; an expected path with no
// file on disk is Missing. The index's own file and any in-flight temp files
// (the .index-*.tmp / .move-*.tmp the atomic writers leave mid-operation) are
// ignored so a concurrent write doesn't masquerade as an orphan.
func (s *Store) Doctor() (Diagnosis, error) {
	all, err := s.idx.List()
	if err != nil {
		return Diagnosis{}, fmt.Errorf("doctor: read index: %w", err)
	}

	// expected maps the absolute content path the index predicts → the record
	// that predicts it, so a disk walk can answer "did anything expect this?"
	// and "is this expected path present?" in one structure.
	expected := make(map[string]index.Scratch, len(all))
	var diag Diagnosis
	for _, sc := range all {
		if sc.Morgued() {
			diag.MorgueCount++
		} else {
			diag.LiveCount++
		}
		expected[s.LivePath(sc)] = sc
	}

	// Track which expected paths we actually saw on disk, to compute Missing as
	// "expected but never seen".
	seen := make(map[string]bool, len(expected))

	for _, area := range []struct {
		dir   string
		label string
	}{
		{s.cfg.ScratchesPath(), "scratches"},
		{s.cfg.MorguePath(), "morgue"},
	} {
		entries, err := os.ReadDir(area.dir)
		if err != nil {
			if os.IsNotExist(err) {
				continue // a not-yet-created dir simply holds nothing
			}
			return Diagnosis{}, fmt.Errorf("doctor: scan %s: %w", area.dir, err)
		}
		for _, e := range entries {
			if e.IsDir() || isTransientFile(e.Name()) {
				continue
			}
			path := filepath.Join(area.dir, e.Name())
			size := fileSize(e)

			if _, ok := expected[path]; ok {
				diag.TrackedSize += size
				seen[path] = true
				continue
			}
			diag.Orphans = append(diag.Orphans, Orphan{Path: path, Area: area.label, Size: size})
			diag.OrphanSize += size
		}
	}

	// Anything the index expected but the walk never saw is missing content.
	for path, sc := range expected {
		if !seen[path] {
			diag.Missing = append(diag.Missing, Missing{Scratch: sc, ExpectedPath: path})
		}
	}

	sortDiagnosis(&diag)
	return diag, nil
}

// isTransientFile reports whether a filename is one of the atomic-write temp
// files scratchpatch creates mid-operation (index saves and cross-device
// moves). These are not orphans — they belong to an in-flight write and vanish
// on their own — so doctor skips them rather than alarming about them.
func isTransientFile(name string) bool {
	for _, prefix := range []string{".index-", ".move-"} {
		if len(name) >= len(prefix) && name[:len(prefix)] == prefix {
			return true
		}
	}
	return false
}

// fileSize returns a directory entry's size, treating an unstattable entry as
// size 0 rather than failing the whole scan — doctor is a best-effort audit and
// a single odd file shouldn't abort the report.
func fileSize(e os.DirEntry) int64 {
	info, err := e.Info()
	if err != nil {
		return 0
	}
	return info.Size()
}

// sortDiagnosis gives the report a deterministic order: orphans by path, missing
// by scratch id. Stable output keeps tests honest and makes the human report
// easy to diff run-to-run.
func sortDiagnosis(d *Diagnosis) {
	sort.SliceStable(d.Orphans, func(i, j int) bool { return d.Orphans[i].Path < d.Orphans[j].Path })
	sort.SliceStable(d.Missing, func(i, j int) bool { return d.Missing[i].Scratch.ID < d.Missing[j].Scratch.ID })
}
