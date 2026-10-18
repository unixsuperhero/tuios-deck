package complete

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// slashOMP is what OMP's own discovery (omp 's skills and slash-command
// capabilities, native provider) offers a prompt typed in cwd:
//
//	skills    <agent dir>/skills/<dir>/SKILL.md and .omp/skills/<dir>/SKILL.md in
//	          cwd and each ancestor up to the repository root (never the home
//	          folder); run as /skill:<name>. The name is the frontmatter name,
//	          else the folder's; a skill with no description, or enabled: false,
//	          is not loaded.
//	commands  <agent dir>/commands/*.md and <cwd>/.omp/commands/*.md, not
//	          recursive; run as /<file name without .md>.
//
// The agent dir is $PI_CODING_AGENT_DIR or ~/.omp/agent. Claude and other
// providers' folders are off by default in OMP and are not listed, nor are
// extension or plugin commands.
func slashOMP(cwd string) []string {
	home, _ := os.UserHomeDir()
	agent := os.Getenv("PI_CODING_AGENT_DIR")
	if agent == "" {
		agent = filepath.Join(home, ".omp", "agent")
	}
	seen := map[string]bool{}
	var out []string
	add := func(name string) {
		if name != "" && !seen[name] {
			seen[name] = true
			out = append(out, name)
		}
	}
	dirs := []string{agent}
	if cwd != home {
		dirs = append([]string{filepath.Join(cwd, ".omp")}, dirs...)
	}
	for _, d := range dirs {
		ompCommands(filepath.Join(d, "commands"), add)
	}
	skillDirs := []string{filepath.Join(agent, "skills")}
	for dir := filepath.Clean(cwd); dir != home; {
		skillDirs = append(skillDirs, filepath.Join(dir, ".omp", "skills"))
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			break
		}
		up := filepath.Dir(dir)
		if up == dir {
			break
		}
		dir = up
	}
	for _, d := range skillDirs {
		ompSkills(d, add)
	}
	sort.Strings(out)
	return out
}

func ompCommands(dir string, add func(string)) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range ents {
		name := e.Name()
		if strings.HasSuffix(name, ".md") && !strings.HasPrefix(name, ".") && !e.IsDir() {
			add(strings.TrimSuffix(name, ".md"))
		}
	}
}

func ompSkills(dir string, add func(string)) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range ents {
		if strings.HasPrefix(e.Name(), ".") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name(), "SKILL.md"))
		if err != nil {
			continue
		}
		name, described, enabled := frontmatter(string(data))
		if !described || !enabled {
			continue
		}
		if name == "" {
			name = e.Name()
		}
		if !strings.ContainsAny(name, `/\`) {
			add("skill:" + name)
		}
	}
}

// frontmatter reads the three keys OMP's skill loader looks at from a file's
// leading `---` block: name, whether description is non-empty, and whether
// the skill is enabled (everything but `enabled: false`).
func frontmatter(text string) (name string, described, enabled bool) {
	enabled = true
	lines := strings.Split(text, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return "", false, true
	}
	for _, l := range lines[1:] {
		if strings.TrimSpace(l) == "---" {
			break
		}
		k, v, ok := strings.Cut(l, ":")
		if !ok || strings.HasPrefix(l, " ") || strings.HasPrefix(l, "\t") {
			continue
		}
		v = strings.Trim(strings.TrimSpace(v), `"'`)
		switch strings.TrimSpace(k) {
		case "name":
			name = v
		case "description":
			// A folded or literal block (> or |) holds its text on later lines.
			described = v != ""
		case "enabled":
			enabled = v != "false"
		}
	}
	return name, described, enabled
}
