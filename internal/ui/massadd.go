package ui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/unixsuperhero/tuios-deck/internal/store"
)

// massHint is the outline format, shown under the mass-add field.
const massHint = `no indent is a project; each 2-space or tab indent nests a task under the line above
existing names are reused and # starts a comment, for example:
deck
  command palette
    verb autocomplete
  captures page
web
  landing`

// outlineLine is one item of a mass-add outline: its line number from 1,
// its text, and the index of the line it nests under, -1 at the top.
type outlineLine struct {
	n      int
	text   string
	parent int
}

// parseOutline reads a mass-add outline. A line with no indent is a
// project, or an existing task named by its path; each further indent
// level, two spaces or a tab, nests a task under the line above it. Blank
// lines and `#` comments are skipped. A problem names its line.
func parseOutline(text string) ([]outlineLine, string) {
	var out []outlineLine
	var stack []int // the last line at each depth
	for i, raw := range strings.Split(text, "\n") {
		n := i + 1
		line := strings.TrimRight(raw, " \t\r")
		body := strings.TrimLeft(line, " \t")
		if body == "" || strings.HasPrefix(body, "#") {
			continue
		}
		lead := line[:len(line)-len(body)]
		spaces := strings.Count(lead, " ")
		if spaces%2 != 0 {
			return nil, fmt.Sprintf("line %d: indent by two spaces or a tab", n)
		}
		depth := spaces/2 + strings.Count(lead, "\t")
		switch {
		case depth > 0 && len(stack) == 0:
			return nil, fmt.Sprintf("line %d: indented under nothing", n)
		case depth > len(stack):
			return nil, fmt.Sprintf("line %d: indented more than one level past the line above", n)
		}
		stack = stack[:depth]
		parent := -1
		if depth > 0 {
			parent = stack[depth-1]
		}
		out = append(out, outlineLine{n: n, text: body, parent: parent})
		stack = append(stack, len(out)-1)
	}
	if len(out) == 0 {
		return nil, "nothing to add"
	}
	return out, ""
}

// checkOutline parses the outline and resolves the task paths written at
// the top, so a refusal comes before anything is created.
func (m *Model) checkOutline(text string) ([]outlineLine, string) {
	lines, msg := parseOutline(text)
	if msg != "" {
		return nil, msg
	}
	for _, l := range lines {
		if l.parent < 0 && strings.Contains(l.text, " / ") {
			if _, msg := m.findTask(l.text); msg != "" {
				return nil, fmt.Sprintf("line %d: %s", l.n, msg)
			}
		}
	}
	return lines, ""
}

// massResult is what an outline added, and the first project and task it
// named: the first one created, or the first one reused when it created
// none. The task is given as its path.
type massResult struct {
	projects, tasks int
	project, task   string
}

// applyOutline creates the outline's items in order. A project or a task
// whose name is already taken under the same parent is reused.
func (m *Model) applyOutline(lines []outlineLine) (massResult, error) {
	type ref struct{ projectID, taskID string }
	refs := make([]ref, len(lines))
	var r massResult
	var newProject, anyProject, newTask, anyTask string
	for i, l := range lines {
		if l.parent < 0 && strings.Contains(l.text, " / ") {
			t, msg := m.findTask(l.text)
			if msg != "" {
				return r, fmt.Errorf("line %d: %s", l.n, msg)
			}
			refs[i] = ref{taskID: t.ID}
			if anyTask == "" {
				anyTask = m.taskPath(*t)
			}
			continue
		}
		if l.parent < 0 {
			p := m.store.ProjectByName(l.text)
			if p == nil {
				np, err := m.store.AddProject(l.text, nil)
				if err != nil {
					return r, fmt.Errorf("line %d: %w", l.n, err)
				}
				p = &np
				r.projects++
				if newProject == "" {
					newProject = p.Name
				}
			}
			if anyProject == "" {
				anyProject = p.Name
			}
			refs[i] = ref{projectID: p.ID}
			continue
		}
		par := refs[l.parent]
		siblings := m.store.Roots(par.projectID)
		if par.taskID != "" {
			siblings = m.store.Children(par.taskID)
		}
		var t *store.Task
		for _, s := range siblings {
			if strings.EqualFold(s.Title, l.text) {
				s := s
				t = &s
				break
			}
		}
		if t == nil {
			nt, err := m.store.AddTask(par.projectID, par.taskID, l.text, nil)
			if err != nil {
				return r, fmt.Errorf("line %d: %w", l.n, err)
			}
			t = &nt
			r.tasks++
			if newTask == "" {
				newTask = m.taskPath(*t)
			}
		}
		if anyTask == "" {
			anyTask = m.taskPath(*t)
		}
		refs[i] = ref{taskID: t.ID}
	}
	r.project, r.task = first(newProject, anyProject), first(newTask, anyTask)
	return r, nil
}

func first(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// openMassAdd opens the mass-add screen with the outline prefilled.
func (m *Model) openMassAdd(prefill string) tea.Cmd {
	f := &form{Title: "add projects and tasks"}
	f.Fields = []field{{Label: "outline", Keep: true, Multi: true, Value: prefill, Hint: massHint}}
	// The raw value is read, not the trimmed one, so the first line's
	// indent and the line numbers stay as typed.
	f.Validate = func([]string) (int, string) {
		_, msg := m.checkOutline(f.Fields[0].Value)
		return 0, msg
	}
	f.Done = func([]string) tea.Cmd {
		lines, _ := m.checkOutline(f.Fields[0].Value)
		r, err := m.applyOutline(lines)
		status := "added " + plural(r.projects, "project") + ", " + plural(r.tasks, "task")
		return func() tea.Msg { return actionMsg{status: status, err: err} }
	}
	m.mode = modeForm
	m.form = f
	return nil
}
