package complete

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// maxFiles caps how many entries of one folder Files lists.
const maxFiles = 200

// Files lists what an `@` word can complete. word is what follows the `@`: a
// path relative to cwd, or absolute, or under `~/`. It returns the folder
// typed so far (as typed, ending in a slash or empty) and the entries of that
// folder whose names start with the rest, folders with a trailing slash and
// hidden entries left out. Names with whitespace are left out too, since the
// harness reads an `@path` up to the next space.
func Files(cwd, word string) (dir string, names []string) {
	i := strings.LastIndex(word, "/")
	dir, prefix := word[:i+1], word[i+1:]
	base := dir
	switch {
	case strings.HasPrefix(dir, "~/"):
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, dir[2:])
	case !filepath.IsAbs(dir):
		base = filepath.Join(cwd, dir)
	}
	ents, err := os.ReadDir(base)
	if err != nil {
		return dir, nil
	}
	for _, e := range ents {
		name := e.Name()
		if strings.HasPrefix(name, ".") && !strings.HasPrefix(prefix, ".") || strings.ContainsAny(name, " \t\n") {
			continue
		}
		if !strings.HasPrefix(strings.ToLower(name), strings.ToLower(prefix)) {
			continue
		}
		isDir := e.IsDir()
		if e.Type()&os.ModeSymlink != 0 {
			if st, err := os.Stat(filepath.Join(base, name)); err == nil {
				isDir = st.IsDir()
			}
		}
		if isDir {
			name += "/"
		}
		names = append(names, name)
	}
	sort.Strings(names)
	if len(names) > maxFiles {
		names = names[:maxFiles]
	}
	return dir, names
}
