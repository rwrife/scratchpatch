// Package config resolves where scratchpatch keeps its data and what the
// out-of-the-box defaults are.
//
// The store lives under an XDG-style data directory and can be relocated
// wholesale with the SCRATCHPATCH_HOME environment variable, which is handy
// for tests and for users who want their scratches somewhere specific.
//
// config touches no scratch content; it only computes paths and constants.
// The store package is the only thing that reads or writes scratch files.
package config

import (
	"os"
	"path/filepath"
	"time"
)

const (
	// EnvHome overrides the entire store location when set.
	EnvHome = "SCRATCHPATCH_HOME"

	// appDir is the per-user subdirectory used under the XDG data home.
	appDir = "scratchpatch"

	// ScratchesDir holds live scratch content.
	ScratchesDir = "scratches"

	// MorgueDir holds soft-deleted scratches awaiting hard-deletion.
	MorgueDir = "morgue"

	// IndexFile is the JSON index filename at the store root.
	IndexFile = "index.json"

	// DefaultExt is the file extension used when `sp new` isn't told otherwise.
	DefaultExt = "md"

	// DefaultTTL is how long a scratch lives before it's eligible for reaping.
	DefaultTTL = 7 * 24 * time.Hour

	// DefaultGrace is how long a soft-deleted scratch lingers in the morgue
	// before `sp reap` is allowed to hard-delete it.
	DefaultGrace = 3 * 24 * time.Hour
)

// Config is the resolved runtime configuration: where things live plus the
// defaults that govern new scratches and reaping.
type Config struct {
	// Home is the store root directory.
	Home string

	// DefaultTTL is the lifespan applied to scratches created without an
	// explicit --ttl.
	DefaultTTL time.Duration

	// DefaultExt is the extension applied to scratches created without an
	// explicit --ext (no leading dot).
	DefaultExt string

	// Grace is the morgue retention window before hard-deletion is allowed.
	Grace time.Duration
}

// Load resolves the configuration from the environment, falling back to
// sensible defaults. It never creates directories; call store.Open (or
// store.Init) to materialize the layout on disk.
func Load() (Config, error) {
	home, err := resolveHome()
	if err != nil {
		return Config{}, err
	}
	return Config{
		Home:       home,
		DefaultTTL: DefaultTTL,
		DefaultExt: DefaultExt,
		Grace:      DefaultGrace,
	}, nil
}

// ScratchesPath is the absolute path to the live-scratches directory.
func (c Config) ScratchesPath() string { return filepath.Join(c.Home, ScratchesDir) }

// MorguePath is the absolute path to the morgue directory.
func (c Config) MorguePath() string { return filepath.Join(c.Home, MorgueDir) }

// IndexPath is the absolute path to the JSON index file.
func (c Config) IndexPath() string { return filepath.Join(c.Home, IndexFile) }

// resolveHome picks the store root: SCRATCHPATCH_HOME if set, else the
// XDG data dir (os.UserConfigDir is the closest stdlib equivalent that
// works cross-platform without extra deps), else a ~/.scratchpatch fallback.
func resolveHome() (string, error) {
	if v := os.Getenv(EnvHome); v != "" {
		abs, err := filepath.Abs(v)
		if err != nil {
			return "", err
		}
		return abs, nil
	}

	// Prefer XDG_DATA_HOME explicitly; it's the spec-correct location for
	// "user-specific data files" like our store. os.UserConfigDir doesn't
	// read XDG_DATA_HOME, so we check it ourselves first.
	if xdg := os.Getenv("XDG_DATA_HOME"); xdg != "" {
		return filepath.Join(xdg, appDir), nil
	}

	if home, err := os.UserHomeDir(); err == nil && home != "" {
		return filepath.Join(home, ".local", "share", appDir), nil
	}

	// Last resort: a relative directory in the working dir. Better than
	// erroring out entirely on exotic environments.
	return filepath.Join(".", "."+appDir), nil
}
