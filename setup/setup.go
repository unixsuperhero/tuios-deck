// Package setup carries the files the deck needs on a machine beyond its own
// binary, and installs, checks and removes them. Today that is one file: the
// oh-my-pi extension that reports an agent's activity to tuios. Every file
// is embedded, so `deck setup install` works from the binary alone.
package setup

import (
	"bytes"
	"embed"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

//go:embed omp
var files embed.FS

// Marker is the line every file written by the deck carries, so status can
// tell the deck's file from one the person wrote with the same name.
const Marker = "TUIOS_DECK_SETUP_ID="

// Item is one file the deck can put on a machine.
type Item struct {
	ID     string
	Title  string
	Source string // the path inside the embedded files
	Target string // where it goes; "~" is the home directory
	// Needs are the preconditions, each a short check and the hint when it fails.
	Needs []Need
	Notes string
}

// Need is one precondition of an item.
type Need struct {
	What  string
	Check func() bool
	Hint  string
}

// State is what status found at an item's target.
type State string

// The states of an item.
const (
	Missing  State = "missing"  // nothing at the target
	Current  State = "current"  // the deck's file, and the same bytes the binary carries
	Outdated State = "outdated" // the deck's file, from an older or edited copy
	Foreign  State = "foreign"  // a file of the same name the deck did not write
)

// Items lists everything the deck can install.
func Items() []Item {
	return []Item{
		{
			ID:     "omp-activity",
			Title:  "oh-my-pi extension that reports prompts, tool calls and turns to tuios",
			Source: "omp/tuios-deck-omp-activity.ts",
			Target: "~/.omp/agent/extensions/tuios-deck-omp-activity.ts",
			Needs: []Need{
				{What: "oh-my-pi's agent folder (~/.omp/agent)", Check: func() bool { return isDir(Expand("~/.omp/agent")) },
					Hint: "install oh-my-pi first, or run omp once so it creates the folder"},
				{What: "tuios on PATH", Check: func() bool { _, err := exec.LookPath("tuios"); return err == nil },
					Hint: "the extension runs `tuios call`; install tuios or set TUIOS_DECK_TUIOS to its path"},
				{What: "tuios's own omp integration (reports the pane's state)", Check: tuiosOMPIntegration,
					Hint: "run `tuios integration install omp`; without it the pane has activity but no state"},
			},
			Notes: "Takes effect in omp sessions started after the install, inside a tuios pane.",
		},
	}
}

// Find returns the item with id.
func Find(id string) (Item, bool) {
	for _, it := range Items() {
		if it.ID == id {
			return it, true
		}
	}
	return Item{}, false
}

// Content is the bytes the binary carries for an item.
func (it Item) Content() []byte {
	b, err := files.ReadFile(it.Source)
	if err != nil {
		panic("setup: embedded file missing: " + it.Source)
	}
	return b
}

// Path is the item's target with "~" expanded.
func (it Item) Path() string { return Expand(it.Target) }

// Status reads the target and classifies it.
func (it Item) Status() (State, error) {
	have, err := os.ReadFile(it.Path())
	if os.IsNotExist(err) {
		return Missing, nil
	}
	if err != nil {
		return "", err
	}
	if !bytes.Contains(have, []byte(Marker+it.ID)) {
		return Foreign, nil
	}
	if bytes.Equal(have, it.Content()) {
		return Current, nil
	}
	return Outdated, nil
}

// Install writes the item, replacing the deck's own older copy and refusing
// to overwrite a foreign file unless force is set. It reports what it did.
func (it Item) Install(force bool) (string, error) {
	st, err := it.Status()
	if err != nil {
		return "", err
	}
	switch st {
	case Current:
		return "already current", nil
	case Foreign:
		if !force {
			return "", fmt.Errorf("%s exists and was not written by the deck; move it aside or pass --force", it.Path())
		}
	}
	dir := filepath.Dir(it.Path())
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	tmp := it.Path() + ".tuios-deck.tmp"
	if err := os.WriteFile(tmp, it.Content(), 0o644); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, it.Path()); err != nil {
		os.Remove(tmp)
		return "", err
	}
	switch st {
	case Missing:
		return "installed", nil
	case Foreign:
		return "replaced a foreign file", nil
	default:
		return "updated", nil
	}
}

// Uninstall removes the item when the deck wrote it. A foreign file is left
// alone and reported.
func (it Item) Uninstall() (string, error) {
	st, err := it.Status()
	if err != nil {
		return "", err
	}
	switch st {
	case Missing:
		return "not installed", nil
	case Foreign:
		return "", fmt.Errorf("%s was not written by the deck; left in place", it.Path())
	}
	if err := os.Remove(it.Path()); err != nil {
		return "", err
	}
	return "removed", nil
}

// UnmetNeeds lists the preconditions that fail now.
func (it Item) UnmetNeeds() []Need {
	var out []Need
	for _, n := range it.Needs {
		if !n.Check() {
			out = append(out, n)
		}
	}
	return out
}

// Expand replaces a leading "~" with the home directory.
func Expand(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			return filepath.Join(home, strings.TrimPrefix(p, "~"))
		}
	}
	return p
}

func isDir(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.IsDir()
}

// tuiosOMPIntegration asks tuios whether its own omp hook is installed.
func tuiosOMPIntegration() bool {
	out, err := exec.Command("tuios", "integration", "status", "omp").CombinedOutput()
	if err != nil {
		return false
	}
	return bytes.Contains(out, []byte("installed"))
}
