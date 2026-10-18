package ui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/unixsuperhero/tuios-deck/internal/store"
)

// massRules is the outline format, shown under the mass-add field: with
// massExample when there is nothing yet, or with massKnown under the
// existing items.
const massRules = `no indent is a project; each 2-space or tab indent nests a task under the line above
arrows move the cursor, enter splits its line, tab/shift+tab indent or outdent it
existing names are reused and # starts a comment`

const massKnown = "existing items are shown dim; editing or removing them changes nothing"

const massExample = `, for example:
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
		if m.isPathLine(l) {
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

// isPathLine says whether an outline line names an existing task by its
// path: a line at the top with " / " in it that is not a project's name.
func (m *Model) isPathLine(l outlineLine) bool {
	return l.parent < 0 && strings.Contains(l.text, " / ") && m.store.ProjectByName(l.text) == nil
}

// applyOutline creates the outline's items in order. A project or a task
// whose name is already taken under the same parent is reused. When it
// creates nothing, the result names the item on line at, where the cursor
// was.
func (m *Model) applyOutline(lines []outlineLine, at int) (massResult, error) {
	type ref struct{ projectID, taskID string }
	refs := make([]ref, len(lines))
	var r massResult
	var newProject, anyProject, newTask, anyTask string
	for i, l := range lines {
		if m.isPathLine(l) {
			t, msg := m.findTask(l.text)
			if msg != "" {
				return r, fmt.Errorf("line %d: %s", l.n, msg)
			}
			refs[i] = ref{taskID: t.ID}
			if l.n == at {
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
			if l.n == at {
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
		if l.n == at {
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

// openMassAdd opens the mass-add screen with every existing project and
// task in the outline, and prefill, an addPrefill outline, merged into it.
func (m *Model) openMassAdd(prefill string) tea.Cmd {
	f := &form{Title: "add projects and tasks"}
	value, pos := m.massOutline(prefill)
	hint := massRules + massExample
	if len(m.store.Projects()) > 0 {
		hint = massRules + "\n" + massKnown
	}
	f.Fields = []field{{Label: "outline", Keep: true, Multi: true, Outline: true, Value: value, pos: pos, Hint: hint, Known: m.knownLines}}
	// The raw value is read, not the trimmed one, so the first line's
	// indent and the line numbers stay as typed.
	f.Validate = func([]string) (int, string) {
		_, msg := m.checkOutline(f.Fields[0].Value)
		return 0, msg
	}
	f.Done = func([]string) tea.Cmd {
		lines, _ := m.checkOutline(f.Fields[0].Value)
		at, _ := f.Fields[0].cursorLine()
		r, err := m.applyOutline(lines, at+1)
		m.resumeWith(r)
		status := "added " + plural(r.projects, "project") + ", " + plural(r.tasks, "task")
		return func() tea.Msg { return actionMsg{status: status, err: err} }
	}
	m.mode = modeForm
	m.form = f
	return nil
}

// addPrefill is the outline merged into the mass-add screen when a form's
// addNew was taken: what was typed, as a project line for a project field,
// or as a task for a task field.
func (m *Model) addPrefill(f *form) string {
	fl := f.Fields[f.adding-1]
	typed := strings.TrimSpace(fl.Value)
	if fl.New == "project" {
		return typed
	}
	return m.taskPrefill(typed)
}

// taskPrefill is a new task's title under the selected pane's task, or
// its project when it has no task, or the first project when the pane has
// neither: the parent's line, a project name or a task path, and the title
// indented under it.
func (m *Model) taskPrefill(typed string) string {
	parent := ""
	if p := m.selected(); p != nil {
		if pr, t, _ := m.store.Resolve(p.ID); t != nil {
			parent = m.taskPath(*t)
		} else if pr != nil {
			parent = pr.Name
		}
	}
	if parent == "" {
		if ps := m.store.Projects(); len(ps) > 0 {
			parent = ps[0].Name
		}
	}
	if parent == "" {
		return typed
	}
	return parent + "\n  " + typed
}

// treeLine is one line of the outline of the store: its depth, its text,
// and the project or task it names.
type treeLine struct {
	depth             int
	text              string
	projectID, taskID string
}

// storeOutline is every project, sorted by name, with its tasks and
// subtasks nested beneath it oldest first, in the grammar parseOutline
// reads. A task whose title starts with # would read as a comment, so it
// goes at the end, at the top, written as its path, with its subtasks
// under it; a project named so is left out.
func (m *Model) storeOutline() []treeLine {
	var out, later []treeLine
	var walk func(t store.Task, depth int, into *[]treeLine)
	walk = func(t store.Task, depth int, into *[]treeLine) {
		if strings.HasPrefix(t.Title, "#") {
			var sub []treeLine
			sub = append(sub, treeLine{text: m.taskPath(t), projectID: t.ProjectID, taskID: t.ID})
			for _, c := range m.store.Children(t.ID) {
				walk(c, 1, &sub)
			}
			later = append(later, sub...)
			return
		}
		*into = append(*into, treeLine{depth: depth, text: t.Title, projectID: t.ProjectID, taskID: t.ID})
		for _, c := range m.store.Children(t.ID) {
			walk(c, depth+1, into)
		}
	}
	for _, p := range m.store.Projects() {
		if strings.HasPrefix(p.Name, "#") {
			continue
		}
		out = append(out, treeLine{text: p.Name, projectID: p.ID})
		for _, t := range m.store.Roots(p.ID) {
			walk(t, 1, &out)
		}
	}
	return append(out, later...)
}

// massOutline is the text the mass-add screen opens with and the cursor
// in it. With no prefill it is the store's outline and an empty line
// after it for the cursor. A prefill from addPrefill, a title alone or a
// parent's line with the title under it, puts the title on a new line
// after the parent's subtree, or at the end when the parent is not found,
// with the cursor at its end.
func (m *Model) massOutline(prefill string) (string, int) {
	lines := m.storeOutline()
	if prefill != "" {
		parts := strings.Split(prefill, "\n")
		typed := strings.TrimSpace(parts[len(parts)-1])
		at, depth := len(lines), 0
		if len(parts) > 1 {
			if i := m.parentLine(lines, strings.TrimSpace(parts[0])); i >= 0 {
				depth = lines[i].depth + 1
				for at = i + 1; at < len(lines) && lines[at].depth >= depth; at++ {
				}
			}
		}
		lines = append(lines[:at], append([]treeLine{{depth: depth, text: typed}}, lines[at:]...)...)
		var b strings.Builder
		pos := 0
		for i, l := range lines {
			if i > 0 {
				b.WriteString("\n")
			}
			b.WriteString(strings.Repeat("  ", l.depth) + l.text)
			if i == at {
				pos = b.Len()
			}
		}
		return b.String(), pos
	}
	if len(lines) == 0 {
		return "", 0
	}
	var b strings.Builder
	for _, l := range lines {
		b.WriteString(strings.Repeat("  ", l.depth) + l.text + "\n")
	}
	return b.String(), b.Len()
}

// parentLine is the index of the line naming the project or the task
// path name, -1 when none does.
func (m *Model) parentLine(lines []treeLine, name string) int {
	projectID, taskID := "", ""
	if p := m.store.ProjectByName(name); p != nil {
		projectID = p.ID
	} else if t, msg := m.findTask(name); msg == "" {
		taskID = t.ID
	} else {
		return -1
	}
	for i, l := range lines {
		if (taskID != "" && l.taskID == taskID) || (taskID == "" && l.taskID == "" && l.projectID == projectID) {
			return i
		}
	}
	return -1
}

// knownLines is, for each line of an outline, the status of the existing
// project or task it names under its parent, or "" for a line that names
// none: a new item, a blank line or a comment. The box draws a known line
// dim. Indent mistakes the parser would refuse just make lines unknown.
func (m *Model) knownLines(text string) []string {
	type ref struct{ projectID, taskID string }
	raw := strings.Split(text, "\n")
	out := make([]string, len(raw))
	var stack []*ref // the item at each depth, nil when the line was new
	for i, l := range raw {
		line := strings.TrimRight(l, " \t\r")
		body := strings.TrimLeft(line, " \t")
		if body == "" || strings.HasPrefix(body, "#") {
			continue
		}
		lead := line[:len(line)-len(body)]
		depth := strings.Count(lead, " ")/2 + strings.Count(lead, "\t")
		if depth > len(stack) {
			stack = append(stack, nil)
			continue
		}
		stack = stack[:depth]
		var found *ref
		switch {
		case depth == 0:
			if p := m.store.ProjectByName(body); p != nil {
				found, out[i] = &ref{projectID: p.ID}, p.Status
			} else if t, msg := m.findTask(body); strings.Contains(body, " / ") && msg == "" {
				found, out[i] = &ref{taskID: t.ID}, t.Status
			}
		case stack[depth-1] != nil:
			par := stack[depth-1]
			siblings := m.store.Roots(par.projectID)
			if par.taskID != "" {
				siblings = m.store.Children(par.taskID)
			}
			for _, s := range siblings {
				if strings.EqualFold(s.Title, body) {
					found, out[i] = &ref{taskID: s.ID}, s.Status
					break
				}
			}
		}
		stack = append(stack, found)
	}
	return out
}

// resumeWith sets the field of the suspended form that opened the
// mass-add screen to the first project or task the outline named. The
// model puts that form back once the mass-add form is done.
func (m *Model) resumeWith(r massResult) {
	if tv, ok := m.protoBack.(*treeView); ok {
		tv.added(r)
	}
	f := m.formStack
	if f == nil || f.adding == 0 {
		return
	}
	fl := &f.Fields[f.adding-1]
	v := r.project
	if fl.New == "task" {
		v = r.task
	}
	if v != "" {
		fl.Value, fl.hl = v, 0
	}
	f.adding = 0
}
