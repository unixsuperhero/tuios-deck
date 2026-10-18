package web

import (
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/unixsuperhero/tuios-deck/internal/complete"
)

// maxSuggest caps how many suggestions one completion answers with.
const maxSuggest = 20

// expandPath turns a leading ~ into the home folder and cleans the path; an
// empty one is the home folder.
func expandPath(p string) string {
	if p == "" || p == "~" || strings.HasPrefix(p, "~/") {
		home, _ := os.UserHomeDir()
		p = home + strings.TrimPrefix(p, "~")
	}
	return filepath.Clean(p)
}

// completeWord answers a prompt box's completion. kind=at lists what an `@`
// word can name under cwd (word is what follows the `@`); kind=slash lists
// the harness's slash commands and skills that contain word. Items are the
// text to put after the `@` or `/`, folders ending in a slash.
func (s *Server) completeWord(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	cwd, word := expandPath(q.Get("cwd")), q.Get("word")
	items := []string{}
	switch q.Get("kind") {
	case "at":
		dir, names := complete.Files(cwd, word)
		for _, n := range names {
			items = append(items, dir+n)
		}
	case "slash":
		for _, n := range complete.Slash(q.Get("harness"), cwd) {
			if strings.Contains(strings.ToLower(n), strings.ToLower(word)) {
				items = append(items, n)
			}
		}
		sort.SliceStable(items, func(i, j int) bool {
			return strings.HasPrefix(items[i], word) && !strings.HasPrefix(items[j], word)
		})
	default:
		writeJSON(w, 400, map[string]any{"error": "kind must be at or slash"})
		return
	}
	if len(items) > maxSuggest {
		items = items[:maxSuggest]
	}
	writeJSON(w, 200, map[string]any{"items": items})
}

// files lists the folders and files under a path for the attachment picker,
// the way dirs lists folders: the browser cannot name a path on this machine.
func (s *Server) files(w http.ResponseWriter, r *http.Request) {
	path := expandPath(r.URL.Query().Get("path"))
	entries, err := os.ReadDir(path)
	if err != nil {
		writeJSON(w, 400, map[string]any{"error": err.Error(), "path": path})
		return
	}
	dirs, files := []string{}, []string{}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".") {
			continue
		}
		isDir := e.IsDir()
		if e.Type()&os.ModeSymlink != 0 {
			if st, err := os.Stat(filepath.Join(path, e.Name())); err == nil {
				isDir = st.IsDir()
			}
		}
		if isDir {
			dirs = append(dirs, e.Name())
		} else {
			files = append(files, e.Name())
		}
	}
	sort.Strings(dirs)
	sort.Strings(files)
	writeJSON(w, 200, map[string]any{"path": path, "parent": filepath.Dir(path), "dirs": dirs, "files": files})
}
