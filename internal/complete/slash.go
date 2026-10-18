// Package complete finds what a prompt can name: the slash commands and
// skills a harness has installed, and the files a prompt can attach.
package complete

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Slash is the slash commands and skills the harness can run for a prompt
// typed in cwd, as names without the leading slash, sorted. Only Claude Code
// has a convention the deck reads (harness "claude" or "claude-code"): ~/.claude and every .claude folder from
// cwd up to the home folder or the filesystem root hold commands/<name>.md
// (a subfolder becomes `folder:name`) and skills/<name>/SKILL.md. Any other
// harness yields nothing rather than a guess.
func Slash(harness, cwd string) []string {
	if harness != "claude" && harness != "claude-code" {
		return nil
	}
	home, _ := os.UserHomeDir()
	roots := []string{filepath.Join(home, ".claude")}
	for dir := filepath.Clean(cwd); dir != "" && dir != home; {
		roots = append(roots, filepath.Join(dir, ".claude"))
		up := filepath.Dir(dir)
		if up == dir {
			break
		}
		dir = up
	}
	seen := map[string]bool{}
	var out []string
	add := func(name string) {
		if name != "" && !seen[name] {
			seen[name] = true
			out = append(out, name)
		}
	}
	for _, root := range roots {
		commands(filepath.Join(root, "commands"), "", add)
		skills(filepath.Join(root, "skills"), add)
	}
	sort.Strings(out)
	return out
}

func commands(dir, prefix string, add func(string)) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range ents {
		name := e.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		full := filepath.Join(dir, name)
		if st, err := os.Stat(full); err == nil && st.IsDir() {
			commands(full, prefix+name+":", add)
		} else if strings.HasSuffix(name, ".md") {
			add(prefix + strings.TrimSuffix(name, ".md"))
		}
	}
}

func skills(dir string, add func(string)) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range ents {
		if strings.HasPrefix(e.Name(), ".") {
			continue
		}
		if _, err := os.Stat(filepath.Join(dir, e.Name(), "SKILL.md")); err == nil {
			add(e.Name())
		}
	}
}
