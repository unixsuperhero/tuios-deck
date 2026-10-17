// Package capture writes pane output to files under the deck's data folder.
package capture

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/unixsuperhero/tuios-deck/internal/store"
)

// Dir is where captures are written: $TUIOS_DECK_CAPTURES, else the
// captures folder under the deck's data folder (store.DataDir).
func Dir() string {
	if d := os.Getenv("TUIOS_DECK_CAPTURES"); d != "" {
		return d
	}
	return filepath.Join(store.DataDir(), "captures")
}

// Write saves content as <dir>/<session>-<name>-<timestamp>.txt and returns
// the path.
func Write(dir, session, name, content string) (string, error) {
	return WriteKind(dir, session, name, "", content)
}

// WriteKind is Write with a kind ("turn", "cmd") after the timestamp:
// <dir>/<session>-<name>-<timestamp>-<kind>.txt. An empty kind is Write.
func WriteKind(dir, session, name, kind, content string) (string, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	stamp := time.Now().Format("20060102-150405")
	file := fmt.Sprintf("%s-%s-%s.txt", slug(session), slug(name), stamp)
	if kind != "" {
		file = fmt.Sprintf("%s-%s-%s-%s.txt", slug(session), slug(name), stamp, slug(kind))
	}
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
