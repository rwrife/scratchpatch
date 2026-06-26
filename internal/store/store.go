// Package store owns the on-disk layout for scratchpatch: the store root,
// the live `scratches/` directory, and the `morgue/` directory, plus the
// metadata index that describes them.
//
// store is the only package that touches scratch *content* on disk. Commands
// go through a Store rather than reaching for the filesystem directly, which
// keeps the "destructive actions are always two-step" rule enforceable in one
// place.
//
// M2 establishes the layout, the config/index wiring, and directory
// bootstrapping. Content operations (create, cat, soft-delete to morgue,
// hard-delete) arrive in M3/M4 and will live here.
package store

import (
	"fmt"
	"os"

	"github.com/rwrife/scratchpatch/internal/config"
	"github.com/rwrife/scratchpatch/internal/index"
)

// dirPerm is used for the store directories. 0o700 keeps scratches private to
// the user — they can contain throwaway secrets, so default to not-world-
// readable.
const dirPerm os.FileMode = 0o700

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
// content operations (added in later milestones) will be methods on Store so
// the filesystem stays encapsulated here.
func (s *Store) Index() index.Store { return s.idx }
