// Package index is the source of truth for scratch metadata.
//
// Metadata lives in a single JSON file (index.json) at the store root. The
// on-disk format is the contract for v0.1; the Store interface keeps that
// contract behind an abstraction so a SQLite (or other) backend can be
// dropped in later without touching callers.
//
// index never touches scratch *content* — that's the store package's job.
// index only reads and writes the metadata file.
package index

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// schemaVersion lets us evolve the on-disk format later without guessing.
const schemaVersion = 1

// ErrNotFound is returned when a lookup by id has no match.
var ErrNotFound = errors.New("scratch not found")

// Scratch is the metadata record for a single scratch file. The content
// itself lives on disk under the store; this struct is everything we track
// about it.
type Scratch struct {
	// ID is the stable, unique identifier (also the on-disk filename stem).
	ID string `json:"id"`

	// Name is the human-facing label (may be empty / non-unique).
	Name string `json:"name"`

	// CreatedAt is when the scratch was created.
	CreatedAt time.Time `json:"createdAt"`

	// TTL is the requested lifespan. Stored as a Go duration string
	// ("168h0m0s") so the index stays human-readable.
	TTL Duration `json:"ttl"`

	// ExpiresAt is CreatedAt + TTL, precomputed so listing/reaping don't
	// have to recompute it (and so a future change to TTL semantics doesn't
	// silently move existing expiries).
	ExpiresAt time.Time `json:"expiresAt"`

	// Tags are free-form labels for filtering.
	Tags []string `json:"tags,omitempty"`

	// Ext is the file extension (no leading dot), e.g. "md".
	Ext string `json:"ext"`

	// OriginCwd is the working directory the scratch was created from, used
	// later for per-project scoping (`sp ls --here`).
	OriginCwd string `json:"originCwd"`

	// Size is the content size in bytes at last write. 0 until content is
	// written (M3+).
	Size int64 `json:"size"`

	// DeletedAt records when the scratch was soft-deleted into the morgue.
	// A nil pointer means the scratch is live; a set value means it lives
	// under morgue/ and is awaiting hard-deletion past the grace window
	// (M5's reap). Kept as a pointer so live scratches omit it entirely
	// rather than carrying a zero timestamp.
	DeletedAt *time.Time `json:"deletedAt,omitempty"`
}

// Live reports whether the scratch is in the live set (not soft-deleted).
func (s Scratch) Live() bool { return s.DeletedAt == nil }

// Morgued reports whether the scratch has been soft-deleted into the morgue.
func (s Scratch) Morgued() bool { return s.DeletedAt != nil }

// file is the serialized shape of index.json.
type file struct {
	Schema    int       `json:"schema"`
	Scratches []Scratch `json:"scratches"`
}

// Store is the metadata persistence abstraction. The JSON implementation is
// the only one for v0.1; keeping callers on this interface means a different
// backend can be substituted later.
type Store interface {
	// List returns all records, sorted newest-first by CreatedAt.
	List() ([]Scratch, error)

	// Get returns the record with the given id, or ErrNotFound.
	Get(id string) (Scratch, error)

	// Put inserts or replaces a record by id and persists the index.
	Put(s Scratch) error

	// Delete removes a record by id and persists the index. Removing a
	// missing id returns ErrNotFound.
	Delete(id string) error
}

// JSONStore is a Store backed by a single JSON file, written atomically via
// a temp file + rename so a crash mid-write can never leave a half-written
// index. It is not safe for concurrent use across processes; v0.1 assumes a
// single CLI invocation at a time.
type JSONStore struct {
	path string
}

// compile-time assertion that JSONStore satisfies Store.
var _ Store = (*JSONStore)(nil)

// OpenJSON returns a JSONStore for the given index path. It does not create
// the file; a missing file is treated as an empty index on first read
// (bootstrap). The parent directory must already exist before Put is called.
func OpenJSON(path string) *JSONStore {
	return &JSONStore{path: path}
}

// load reads and decodes the index, bootstrapping an empty one when the file
// doesn't exist yet.
func (s *JSONStore) load() (file, error) {
	b, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return file{Schema: schemaVersion}, nil
	}
	if err != nil {
		return file{}, fmt.Errorf("read index: %w", err)
	}

	// An empty file is also a valid "no scratches yet" state.
	if len(b) == 0 {
		return file{Schema: schemaVersion}, nil
	}

	var f file
	if err := json.Unmarshal(b, &f); err != nil {
		return file{}, fmt.Errorf("parse index %s: %w", s.path, err)
	}
	if f.Schema == 0 {
		f.Schema = schemaVersion
	}
	return f, nil
}

// save encodes and atomically writes the index: write to a temp file in the
// same directory, fsync, then rename over the target. The same-dir temp keeps
// the rename atomic (no cross-device move).
func (s *JSONStore) save(f file) error {
	f.Schema = schemaVersion

	b, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return fmt.Errorf("encode index: %w", err)
	}
	b = append(b, '\n')

	dir := filepath.Dir(s.path)
	tmp, err := os.CreateTemp(dir, ".index-*.json.tmp")
	if err != nil {
		return fmt.Errorf("create temp index: %w", err)
	}
	tmpName := tmp.Name()
	// Best-effort cleanup if we bail before the rename.
	defer func() { _ = os.Remove(tmpName) }()

	if _, err := tmp.Write(b); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temp index: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("sync temp index: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp index: %w", err)
	}

	if err := os.Rename(tmpName, s.path); err != nil {
		return fmt.Errorf("commit index: %w", err)
	}
	return nil
}

// List returns all records sorted newest-first (ties broken by id for a
// stable order).
func (s *JSONStore) List() ([]Scratch, error) {
	f, err := s.load()
	if err != nil {
		return nil, err
	}
	out := make([]Scratch, len(f.Scratches))
	copy(out, f.Scratches)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].ID < out[j].ID
		}
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	return out, nil
}

// Get returns the record with the given id.
func (s *JSONStore) Get(id string) (Scratch, error) {
	f, err := s.load()
	if err != nil {
		return Scratch{}, err
	}
	for _, sc := range f.Scratches {
		if sc.ID == id {
			return sc, nil
		}
	}
	return Scratch{}, fmt.Errorf("%q: %w", id, ErrNotFound)
}

// Put inserts or replaces the record with s.ID.
func (s *JSONStore) Put(sc Scratch) error {
	if sc.ID == "" {
		return errors.New("index: scratch id must not be empty")
	}
	f, err := s.load()
	if err != nil {
		return err
	}
	replaced := false
	for i := range f.Scratches {
		if f.Scratches[i].ID == sc.ID {
			f.Scratches[i] = sc
			replaced = true
			break
		}
	}
	if !replaced {
		f.Scratches = append(f.Scratches, sc)
	}
	return s.save(f)
}

// Delete removes the record with the given id.
func (s *JSONStore) Delete(id string) error {
	f, err := s.load()
	if err != nil {
		return err
	}
	idx := -1
	for i := range f.Scratches {
		if f.Scratches[i].ID == id {
			idx = i
			break
		}
	}
	if idx < 0 {
		return fmt.Errorf("%q: %w", id, ErrNotFound)
	}
	f.Scratches = append(f.Scratches[:idx], f.Scratches[idx+1:]...)
	return s.save(f)
}
