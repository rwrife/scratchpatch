package config

import (
	"path/filepath"
	"testing"
	"time"
)

func TestLoadDefaults(t *testing.T) {
	// Isolate from the host environment.
	t.Setenv(EnvHome, "")
	t.Setenv("XDG_DATA_HOME", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.DefaultTTL != 7*24*time.Hour {
		t.Errorf("DefaultTTL = %s, want 168h", cfg.DefaultTTL)
	}
	if cfg.DefaultExt != "md" {
		t.Errorf("DefaultExt = %q, want md", cfg.DefaultExt)
	}
	if cfg.Grace != 3*24*time.Hour {
		t.Errorf("Grace = %s, want 72h", cfg.Grace)
	}
	if cfg.Home == "" {
		t.Error("Home should not be empty")
	}
}

func TestHomeOverride(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(EnvHome, dir)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Home != dir {
		t.Fatalf("Home = %q, want %q", cfg.Home, dir)
	}
	if got, want := cfg.ScratchesPath(), filepath.Join(dir, ScratchesDir); got != want {
		t.Errorf("ScratchesPath = %q, want %q", got, want)
	}
	if got, want := cfg.MorguePath(), filepath.Join(dir, MorgueDir); got != want {
		t.Errorf("MorguePath = %q, want %q", got, want)
	}
	if got, want := cfg.IndexPath(), filepath.Join(dir, IndexFile); got != want {
		t.Errorf("IndexPath = %q, want %q", got, want)
	}
}

func TestXDGDataHome(t *testing.T) {
	t.Setenv(EnvHome, "")
	xdg := t.TempDir()
	t.Setenv("XDG_DATA_HOME", xdg)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := filepath.Join(xdg, appDir)
	if cfg.Home != want {
		t.Fatalf("Home = %q, want %q", cfg.Home, want)
	}
}

func TestHomeOverrideRelativeBecomesAbsolute(t *testing.T) {
	t.Setenv(EnvHome, "relative/store")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !filepath.IsAbs(cfg.Home) {
		t.Fatalf("Home should be absolute, got %q", cfg.Home)
	}
}
