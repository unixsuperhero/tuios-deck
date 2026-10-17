// Package capture writes pane output to files under the deck's data folder.
package capture

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Dir is where captures are written: $TUIOS_DECK_CAPTURES, else
// $XDG_DATA_HOME/tuios-deck/captures, else ~/.local/share/tuios-deck/captures.
func Dir() string {
	if d := os.Getenv("TUIOS_DECK_CAPTURES"); d != "" {
		return d
	}
	base := os.Getenv("XDG_DATA_HOME")
	if base == "" {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, ".local", "share")
	}
	return filepath.Join(base, "tuios-deck", "captures")
}

// Write saves content as <dir>/<session>-<name>-<timestamp>.txt and returns
// the path.
func Write(dir, session, name, content string) (string, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	stamp := time.Now().Format("20060102-150405")
	file := fmt.Sprintf("%s-%s-%s.txt", slug(session), slug(name), stamp)
	path := filepath.Join(dir, file)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		return "", err
	}
	return path, nil
}

func slug(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case b.Len() > 0 && b.String()[b.Len()-1] != '-':
			b.WriteByte('-')
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "pane"
	}
	if len(out) > 40 {
		out = out[:40]
	}
	return out
}
