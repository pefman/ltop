package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSaveLoadRoundTrip(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	if _, err := Load(); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Load on empty dir = %v, want ErrNotFound", err)
	}

	want := Config{
		Endpoint:       "http://127.0.0.1:11436",
		PollIntervalMS: 500,
		Currency:       "SEK",
		KWhPrice:       1.5,
	}
	if err := Save(want); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.Endpoint != want.Endpoint || got.PollIntervalMS != want.PollIntervalMS ||
		got.Currency != want.Currency || got.KWhPrice != want.KWhPrice {
		t.Errorf("Load = %+v, want %+v", got, want)
	}
}

// The config file may hold a private endpoint, so it must not be world readable.
func TestSaveIsOwnerOnly(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if err := Save(Config{Endpoint: "http://h:1"}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	path, err := Path()
	if err != nil {
		t.Fatalf("Path: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("permissions = %o, want 600", perm)
	}
}

// A stored endpoint that no longer parses must trigger the setup wizard rather
// than starting the dashboard against a broken URL.
func TestLoadRejectsInvalidEndpoint(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	path := filepath.Join(dir, "ltop", "config.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"endpoint":""}`), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := Load(); !errors.Is(err, ErrNotFound) {
		t.Errorf("Load = %v, want ErrNotFound", err)
	}
}

func TestLoadNormalisesV1Suffix(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	path := filepath.Join(dir, "ltop", "config.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"endpoint":"http://localhost:11436/v1"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.Endpoint != "http://localhost:11436" {
		t.Errorf("Endpoint = %q, want the /v1 suffix trimmed", got.Endpoint)
	}
}

func TestPollIntervalClamped(t *testing.T) {
	cases := map[int]time.Duration{
		0:      DefaultPollInterval,
		50:     DefaultPollInterval,
		500:    500 * time.Millisecond,
		2000:   2 * time.Second,
		999999: 10 * time.Second,
	}
	for ms, want := range cases {
		if got := (Config{PollIntervalMS: ms}).PollInterval(); got != want {
			t.Errorf("PollInterval(%d) = %v, want %v", ms, got, want)
		}
	}
}

// With no servers running and no input, setup must fail cleanly rather than
// loop or hang.
func TestSetupWithoutInputFails(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	var out strings.Builder
	_, err := Setup(t.Context(), strings.NewReader(""), &out)
	if err == nil {
		t.Fatal("Setup succeeded with no input")
	}
	if !strings.Contains(out.String(), "Scanning") {
		t.Errorf("wizard produced no scan output: %q", out.String())
	}
}

func TestSetupAcceptsTypedURL(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	var out strings.Builder
	cfg, err := Setup(t.Context(), strings.NewReader("http://example.test:9999/v1\n"), &out)
	if err != nil {
		t.Fatalf("Setup: %v", err)
	}
	if cfg.Endpoint != "http://example.test:9999" {
		t.Errorf("Endpoint = %q", cfg.Endpoint)
	}

	saved, err := Load()
	if err != nil {
		t.Fatalf("Load after Setup: %v", err)
	}
	if saved.Endpoint != cfg.Endpoint {
		t.Errorf("saved endpoint = %q, want %q", saved.Endpoint, cfg.Endpoint)
	}
}
