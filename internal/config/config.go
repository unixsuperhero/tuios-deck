// Package config is the deck's settings file: one JSON document at
// $XDG_CONFIG_HOME/tuios-deck/config (default ~/.config/tuios-deck/config).
// Settings are separate from the data store: the store holds projects and
// tasks, this holds how the deck behaves.
package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

// Config is every setting. The zero value is the deck's defaults, so a
// missing file and an empty `{}` mean the same thing.
type Config struct {
	// VimModes gives forms explicit normal and insert modes.
	VimModes bool `json:"vim_modes"`
	// Keys are the rebindings: mode name, then action name, then the key.
	// An action not listed keeps its default key.
	Keys map[string]map[string]string `json:"keys,omitempty"`
	// StaleAfter is how long a pane may go without activity before the
	// deck marks it stale and offers to close it: a Go duration ("45m",
	// "2h"). Empty is the default, DefaultStaleAfter; "0" turns it off.
	StaleAfter string `json:"stale_after,omitempty"`
}

// DefaultStaleAfter is the stale threshold when the file does not set one.
const DefaultStaleAfter = 45 * time.Minute

// StaleThreshold is the threshold in effect; 0 means never stale.
func (c *Config) StaleThreshold() time.Duration {
	if c == nil || c.StaleAfter == "" {
		return DefaultStaleAfter
	}
	d, err := time.ParseDuration(c.StaleAfter)
	if err != nil || d < 0 {
		return DefaultStaleAfter
	}
	return d
}

// Dir is the settings folder.
func Dir() string {
	if x := os.Getenv("XDG_CONFIG_HOME"); x != "" {
		return filepath.Join(x, "tuios-deck")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	return filepath.Join(home, ".config", "tuios-deck")
}

// DefaultPath is the settings file.
func DefaultPath() string { return filepath.Join(Dir(), "config") }

// Load reads the file at path, which holds exactly one JSON document. A missing file is the defaults; a file that
// does not parse, or names a setting that does not exist, is an error that
// names the file.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return &Config{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("settings %s: %w", path, err)
	}
	c := &Config{}
	if len(bytes.TrimSpace(data)) == 0 {
		return c, nil
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(c); err != nil {
		return nil, fmt.Errorf("settings %s: %w", path, err)
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, fmt.Errorf("settings %s: unexpected data after the JSON document", path)
	}
	if err := c.Validate(); err != nil {
		return nil, fmt.Errorf("settings %s: %w", path, err)
	}
	return c, nil
}

// Save writes the settings whole and atomically: a temp file in the same
// folder, then a rename.
func (c *Config) Save(path string) error {
	if err := c.Validate(); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".config-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// Validate checks the settings; keybindings are checked in keys.go.
func (c *Config) Validate() error {
	if c.StaleAfter != "" {
		d, err := time.ParseDuration(c.StaleAfter)
		if err != nil || d < 0 {
			return fmt.Errorf("stale_after is a duration such as 45m or 2h, or 0 to turn it off, not %q", c.StaleAfter)
		}
	}
	return c.validateKeys()
}
