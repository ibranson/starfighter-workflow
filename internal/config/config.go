// Package config loads and validates the daemon's runtime configuration.
//
// Config is sourced from a JSON file (default /etc/sfworkflowd/config.json),
// with sensible defaults applied for any field left unset. On first boot the
// file may not exist; Load returns defaults in that case so the daemon can
// come up far enough to run the first-boot setup flow.
//
// This daemon is a pure network service: it owns no console, no HDMI, no
// display. It is designed to run alongside the starfighters A/V daemon on the
// same Pi without contention — hence a distinct data dir, config path, and
// (critically) a distinct listen port from the A/V app's :8080.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

type Config struct {
	// DataDir is where SQLite and other runtime state live.
	DataDir string `json:"data_dir"`

	// DisplayName is the human-readable name for this instance, shown in
	// the UI header. May be edited from the dashboard later.
	DisplayName string `json:"display_name"`

	HTTP HTTPConfig `json:"http"`
}

type HTTPConfig struct {
	// Addr is the listen address. Defaults to :9090 to stay clear of the
	// A/V daemon's :8080 when both run on the same Pi.
	Addr string `json:"addr"`
}

func defaults() Config {
	return Config{
		DataDir:     "/var/lib/sfworkflowd",
		DisplayName: "Repair Workflow",
		HTTP: HTTPConfig{
			Addr: ":9090",
		},
	}
}

func Load(path string) (Config, error) {
	cfg := defaults()

	b, err := os.ReadFile(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return cfg, nil
	case err != nil:
		return cfg, fmt.Errorf("read %s: %w", path, err)
	}

	if err := json.Unmarshal(b, &cfg); err != nil {
		return cfg, fmt.Errorf("parse %s: %w", path, err)
	}

	if cfg.DataDir == "" {
		return cfg, errors.New("config: data_dir must be set")
	}
	cfg.DataDir = filepath.Clean(cfg.DataDir)
	return cfg, nil
}

// UpdateDisplayName rewrites just the display_name field of the config file at
// path, preserving everything else. Atomic (temp file + rename).
func UpdateDisplayName(path, name string) error {
	return updateConfig(path, func(c *Config) { c.DisplayName = name })
}

func updateConfig(path string, patch func(*Config)) error {
	cfg, err := Load(path)
	if err != nil {
		return err
	}
	patch(&cfg)

	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	b = append(b, '\n')

	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".sfworkflowd-config-*.json")
	if err != nil {
		return fmt.Errorf("create temp: %w", err)
	}
	tmpName := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpName)
		}
	}()

	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return fmt.Errorf("write temp: %w", err)
	}
	if err := tmp.Chmod(0o644); err != nil {
		tmp.Close()
		return fmt.Errorf("chmod temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("rename: %w", err)
	}
	cleanup = false
	return nil
}
