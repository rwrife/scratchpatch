// Package store owns the on-disk layout for scratchpatch: the store root,
// the live `scratches/` directory, and the `morgue/` directory, plus the
// metadata index that describes them.
//
// store is the only package that touches scratch *content* on disk. Commands
// go through a Store rather than reaching for the filesystem directly, which
// keeps the "destructive actions are always two-step" rule enforceable in one
// place.
//
// M2 established the layout, the config/index wiring, and directory
// bootstrapping. M3 adds content creation (`sp new`); soft-delete to morgue,
// hard-delete, and resurrect arrive in M4/M5.
package store

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/rwrife/scratchpatch/internal/config"
	"github.com/rwrife/scratchpatch/internal/index"
)

// dirPerm is used for the store directories. 0o700 keeps scratches private to
// the user — they can contain throwaway secrets, so default to not-world-
// readable.
const dirPerm os.FileMode = 0o700

// filePerm matches dirPerm's intent for scratch content: owner-only.
const filePerm os.FileMode = 0o600

// idBytes is the number of random bytes behind a scratch id (hex-encoded, so
// the id string is twice this long). 4 bytes → 8 hex chars, plenty of room to
// avoid collisions for a personal scratch store while staying short enough to
// type.
const idBytes = 4

// Store is the handle through which commands interact with the scratch store.
// It bundles the resolved config with the metadata index.
type Store struct {
	cfg config.Config
	idx index.Store
}

// Open resolves config, ensures the store layout exists on disk, and returns
// a ready-to-use Store. It is safe to call repeatedly (directory creation is
// idempotent).
func Open() (*Store, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}
	return OpenWith(cfg)
}

// OpenWith is like Open but uses a caller-supplied config. Tests use this to
// point the store at a temp directory without touching the environment.
func OpenWith(cfg config.Config) (*Store, error) {
	if err := ensureLayout(cfg); err != nil {
		return nil, err
	}
	return &Store{
		cfg: cfg,
		idx: index.OpenJSON(cfg.IndexPath()),
	}, nil
}

// ensureLayout creates the store root and its scratches/ + morgue/
// subdirectories if they don't already exist.
func ensureLayout(cfg config.Config) error {
	for _, dir := range []string{cfg.Home, cfg.ScratchesPath(), cfg.MorguePath()} {
		if err := os.MkdirAll(dir, dirPerm); err != nil {
			return fmt.Errorf("create store dir %s: %w", dir, err)
		}
	}
	return nil
}

// Config returns the resolved configuration backing this store.
func (s *Store) Config() config.Config { return s.cfg }

// Index returns the metadata index. Commands use this for listing and lookup;
// content operations are methods on Store so the filesystem stays encapsulated
// here.
func (s *Store) Index() index.Store { return s.idx }

// CreateOptions describes a scratch to create. Zero-value fields fall back to
// store/config defaults (ext, ttl) or are simply left empty (name, tags).
type CreateOptions struct {
	// Name is the human-facing label. Empty means "auto-generate a dated
	// slug" — handled by the caller before Create, or left blank here.
	Name string

	// Ext is the file extension without a leading dot. Empty uses the
	// configured default.
	Ext string

	// TTL is the requested lifespan. Zero uses the configured default.
	TTL time.Duration

	// Tags are free-form labels.
	Tags []string

	// OriginCwd is the directory the scratch was created from. Empty means
	// "use the process working directory".
	OriginCwd string
}

// Create materializes a new scratch: it allocates a unique id, writes an empty
// content file under scratches/, and records the metadata in the index. It
// returns the persisted Scratch (including the resolved id, ext, expiry) and
// the absolute path to its content file.
//
// The file is created empty; `sp new` then opens it in $EDITOR and calls
// Touch to refresh the recorded size once the user saves.
func (s *Store) Create(opts CreateOptions) (index.Scratch, string, error) {
	id, err := s.allocID()
	if err != nil {
		return index.Scratch{}, "", err
	}

	ext := strings.TrimPrefix(opts.Ext, ".")
	if ext == "" {
		ext = s.cfg.DefaultExt
	}

	ttl := opts.TTL
	if ttl <= 0 {
		ttl = s.cfg.DefaultTTL
	}

	cwd := opts.OriginCwd
	if cwd == "" {
		if wd, werr := os.Getwd(); werr == nil {
			cwd = wd
		}
	}

	now := time.Now()
	sc := index.Scratch{
		ID:        id,
		Name:      opts.Name,
		CreatedAt: now,
		TTL:       index.Duration(ttl),
		ExpiresAt: now.Add(ttl),
		Tags:      normalizeTags(opts.Tags),
		Ext:       ext,
		OriginCwd: cwd,
		Size:      0,
	}

	path := s.contentPath(sc)

	// Create the content file exclusively so we never clobber an existing
	// scratch that happens to share an id (belt-and-suspenders with allocID).
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, filePerm)
	if err != nil {
		return index.Scratch{}, "", fmt.Errorf("create scratch file: %w", err)
	}
	if cerr := f.Close(); cerr != nil {
		_ = os.Remove(path)
		return index.Scratch{}, "", fmt.Errorf("close scratch file: %w", cerr)
	}

	if err := s.idx.Put(sc); err != nil {
		// Roll back the orphaned content file so the store stays consistent.
		_ = os.Remove(path)
		return index.Scratch{}, "", fmt.Errorf("record scratch metadata: %w", err)
	}

	return sc, path, nil
}

// Touch refreshes the recorded content size for a scratch from what's on disk
// and persists it. Called after $EDITOR returns so `sp ls` reflects real size.
// A missing content file is treated as size 0 rather than an error.
func (s *Store) Touch(id string) (index.Scratch, error) {
	sc, err := s.idx.Get(id)
	if err != nil {
		return index.Scratch{}, err
	}
	if info, statErr := os.Stat(s.contentPath(sc)); statErr == nil {
		sc.Size = info.Size()
	} else {
		sc.Size = 0
	}
	if err := s.idx.Put(sc); err != nil {
		return index.Scratch{}, err
	}
	return sc, nil
}

// ContentPath returns the absolute path to a scratch's live content file.
func (s *Store) ContentPath(sc index.Scratch) string { return s.contentPath(sc) }

// contentPath is the on-disk location for a scratch's content: id.ext under
// scratches/.
func (s *Store) contentPath(sc index.Scratch) string {
	name := sc.ID
	if sc.Ext != "" {
		name += "." + sc.Ext
	}
	return filepath.Join(s.cfg.ScratchesPath(), name)
}

// allocID returns a random hex id that isn't already present in the index.
func (s *Store) allocID() (string, error) {
	for attempt := 0; attempt < 16; attempt++ {
		buf := make([]byte, idBytes)
		if _, err := rand.Read(buf); err != nil {
			return "", fmt.Errorf("generate scratch id: %w", err)
		}
		id := hex.EncodeToString(buf)
		if _, err := s.idx.Get(id); err != nil {
			// Get returns ErrNotFound for a free id; anything else is real.
			return id, nil
		}
	}
	return "", fmt.Errorf("could not allocate a unique scratch id after 16 attempts")
}

// normalizeTags trims, drops empties, and de-duplicates tags while preserving
// first-seen order, so `--tag a --tag a --tag ""` becomes ["a"].
func normalizeTags(tags []string) []string {
	if len(tags) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(tags))
	out := make([]string, 0, len(tags))
	for _, t := range tags {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		if _, dup := seen[t]; dup {
			continue
		}
		seen[t] = struct{}{}
		out = append(out, t)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
