package capture

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Entry is one capture file in the folder. Session, Pane and Kind are read
// back from the name Write or WriteKind gives a file, best effort: slugs may
// hold dashes, so the first dash-separated part is taken as the session, the
// rest up to the timestamp as the pane name, and a word after the timestamp
// as the kind ("turn", "cmd"; empty for a capture taken by hand).
type Entry struct {
	Name     string    `json:"name"`
	Path     string    `json:"path"`
	Size     int64     `json:"size"`
	Modified time.Time `json:"modified"`
	Session  string    `json:"session"`
	Pane     string    `json:"pane"`
	Kind     string    `json:"kind"`
}

// ErrName is the error for a capture name that is not a bare file name.
var ErrName = errors.New("capture name must be a bare file name")

var stamp = regexp.MustCompile(`-(\d{8})-(\d{6})(?:-([a-z0-9]+))?\.txt$`)

// ValidName says whether name is a bare file name under the folder: no
// separators, no "..", not empty.
func ValidName(name string) bool {
	return name != "" && name != "." && name != ".." &&
		!strings.ContainsAny(name, `/\`) && !strings.Contains(name, "..")
}

// List is every .txt capture in dir, newest first. A missing folder is an
// empty list.
func List(dir string) ([]Entry, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return []Entry{}, nil
	}
	if err != nil {
		return nil, err
	}
	out := []Entry{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".txt") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		entry := Entry{Name: e.Name(), Path: filepath.Join(dir, e.Name()), Size: info.Size(), Modified: info.ModTime()}
		entry.Session, entry.Pane, entry.Kind = parseName(e.Name())
		out = append(out, entry)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if !out[i].Modified.Equal(out[j].Modified) {
			return out[i].Modified.After(out[j].Modified)
		}
		return out[i].Name > out[j].Name
	})
	return out, nil
}

// Matches says whether the entry was written from the pane called name in
// session: the slugs Write made of them, joined, are the entry's.
func (e Entry) Matches(session, name string) bool {
	return slug(session)+"-"+slug(name) == e.Session+"-"+e.Pane
}

func parseName(name string) (session, pane, kind string) {
	base := name
	if m := stamp.FindStringSubmatchIndex(name); m != nil {
		base = name[:m[0]]
		if m[6] >= 0 {
			kind = name[m[6]:m[7]]
		}
	} else {
		base = strings.TrimSuffix(name, ".txt")
	}
	session, pane, _ = strings.Cut(base, "-")
	return session, pane, kind
}

// Read is the content of the capture called name in dir.
func Read(dir, name string) (string, error) {
	if !ValidName(name) {
		return "", ErrName
	}
	data, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// Remove deletes the capture called name in dir.
func Remove(dir, name string) error {
	if !ValidName(name) {
		return ErrName
	}
	return os.Remove(filepath.Join(dir, name))
}
