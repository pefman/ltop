// Package config persists ltop's endpoint selection under the XDG config dir.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/pefman/ltop/internal/llama"
)

// Config is the on-disk settings document.
type Config struct {
	Endpoint       string `json:"endpoint"`
	PollIntervalMS int    `json:"poll_interval_ms"`
}

// DefaultPollInterval matches htop's refresh cadence and is cheap against
// llama.cpp's /metrics and /slots handlers.
const DefaultPollInterval = time.Second

// PollInterval returns the configured cadence, clamped to a sane range.
func (c Config) PollInterval() time.Duration {
	d := time.Duration(c.PollIntervalMS) * time.Millisecond
	switch {
	case d < 200*time.Millisecond:
		return DefaultPollInterval
	case d > 10*time.Second:
		return 10 * time.Second
	default:
		return d
	}
}

// Path returns the config file location, honouring XDG_CONFIG_HOME.
func Path() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "ltop", "config.json"), nil
}

// ErrNotFound reports that no config file exists yet.
var ErrNotFound = errors.New("config not found")

// Load reads the stored config. It returns ErrNotFound on first run.
func Load() (Config, error) {
	path, err := Path()
	if err != nil {
		return Config{}, err
	}
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Config{}, ErrNotFound
	}
	if err != nil {
		return Config{}, err
	}

	var c Config
	if err := json.Unmarshal(b, &c); err != nil {
		return Config{}, fmt.Errorf("%s: %w", path, err)
	}
	if !llama.ValidBase(c.Endpoint) {
		return Config{}, ErrNotFound
	}
	c.Endpoint = llama.NormalizeBase(c.Endpoint)
	return c, nil
}

// Save writes the config atomically with owner-only permissions.
func Save(c Config) error {
	path, err := Path()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}

	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')

	tmp, err := os.CreateTemp(filepath.Dir(path), ".config-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())

	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
