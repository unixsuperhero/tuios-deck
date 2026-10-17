package ui

import (
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/unixsuperhero/tuios-deck/internal/store"
)

// taskPath is a task named by its project and its ancestors, as
// "project / top task / … / title", so subtasks with the same title tell
// apart.
func (m *Model) taskPath(t store.Task) string {
	parts := []string{"?"}
	if p := m.store.Project(m.store.ProjectOf(t.ID)); p != nil {
		parts[0] = p.Name
	}
	for _, c := range m.store.Path(t.ID) {
		parts = append(parts, c.Title)
	}
	return strings.Join(parts, " / ")
}

// taskOptions is every task's path, sorted, for completion.
func (m *Model) taskOptions() []string {
	var out []string
	for _, t := range m.store.Tasks() {
		out = append(out, m.taskPath(t))
	}
	sort.Strings(out)
	return out
}

// projectOptions is every project name, for completion.
func (m *Model) projectOptions() []string {
	var out []string
	for _, p := range m.store.Projects() {
		out = append(out, p.Name)
	}
	return out
}

// findTask resolves what the person typed in a task field: a full path or
// a bare title, either one ignoring case. The title must name one task.
func (m *Model) findTask(text string) (*store.Task, string) {
	text = strings.TrimSpace(text)
	var byPath, byTitle []store.Task
	for _, t := range m.store.Tasks() {
		if strings.EqualFold(m.taskPath(t), text) {
			byPath = append(byPath, t)
		}
		if strings.EqualFold(t.Title, text) {
			byTitle = append(byTitle, t)
		}
	}
	switch {
	case len(byPath) == 1:
		return &byPath[0], ""
	case len(byTitle) == 1:
		return &byTitle[0], ""
	case len(byTitle) > 1:
		return nil, "task " + text + " is ambiguous; give its path (tab lists them)"
	}
	return nil, "no task " + text
}

// resolveTarget turns the task and project fields of a form into the ids
// to assign. A task implies its project, so with a task the project is
// ignored; without one the project is required unless optional. On a
// problem it names the field, taskField or projectField, and the message.
func (m *Model) resolveTarget(task, project string, taskField, projectField int, optional bool) (taskID, projectID string, field int, msg string) {
	if task != "" {
		t, err := m.findTask(task)
		if err != "" {
			return "", "", taskField, err
		}
		return t.ID, "", 0, ""
	}
	if project == "" {
		if optional {
			return "", "", 0, ""
		}
		return "", "", projectField, "a task or a project is required"
	}
	p := m.store.ProjectByName(project)
	if p == nil {
		return "", "", projectField, "no project " + project
	}
	return "", p.ID, 0, ""
}

// targetFields are the task and project fields a form takes after cwd. The
// project defaults to the selected pane's project, kept as a hint only
// when keep is set so an empty field stays empty.
func (m *Model) targetFields(keep bool) []field {
	project := ""
	if p := m.selected(); p != nil {
		if pr, _, _ := m.store.Resolve(p.ID); pr != nil {
			project = pr.Name
		}
	}
	return []field{
		{Label: "task", Keep: true, Options: m.taskOptions},
		{Label: "project", Default: project, Keep: keep, Options: m.projectOptions},
	}
}

// assignment is the theme and the text a pane's assignment is drawn with:
// the task's, or the project's when the pane has only a project.
type assignment struct {
	glyph string
	style lipgloss.Style
	short string // the task title or the project name
	path  string // "project › top task › task"
}

func (m *Model) assignmentOf(windowID string) (assignment, bool) {
	project, task, _ := m.store.Resolve(windowID)
	if project == nil && task == nil {
		return assignment{}, false
	}
	var a assignment
	if project != nil {
		a.glyph, a.style = project.Theme.Glyph, themeStyle(project.Theme)
		a.short, a.path = project.Name, project.Name
	}
	if task != nil {
		a.glyph, a.style, a.short = task.Theme.Glyph, themeStyle(task.Theme), task.Title
		for _, c := range m.store.Path(task.ID) {
			a.path += " › " + c.Title
		}
	}
	if a.glyph == "" {
		a.glyph = "●"
	}
	return a, true
}

func themeStyle(t store.Theme) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(lipgloss.Color(t.ANSI()))
}

// openAssignForm assigns, or reassigns, the selected pane to a task or a
// project; both left empty drops the assignment.
func (m *Model) openAssignForm() tea.Cmd {
	p := m.selected()
	if p == nil {
		m.setStatus("select a pane first")
		return nil
	}
	windowID, session, name := p.ID, p.Session, p.Name
	m.mode = modeForm
	m.form = &form{
		Title:  "assign " + name,
		Fields: m.targetFields(true),
		Validate: func(v []string) (int, string) {
			_, _, f, msg := m.resolveTarget(v[0], v[1], 0, 1, true)
			return f, msg
		},
		Done: func(v []string) tea.Cmd {
			taskID, projectID, _, _ := m.resolveTarget(v[0], v[1], 0, 1, true)
			return func() tea.Msg {
				if taskID == "" && projectID == "" {
					if err := m.store.Unassign(windowID); err != nil {
						return actionMsg{err: err}
					}
					return actionMsg{status: "unassigned " + name}
				}
				if err := m.store.Assign(windowID, session, taskID, projectID); err != nil {
					return actionMsg{err: err}
				}
				a, _ := m.assignmentOf(windowID)
				return actionMsg{status: "assigned " + name + " to " + a.path}
			}
		},
	}
	return nil
}

// assign ties a window the daemon just made to the target the form gave,
// and says so after the daemon's own report.
func (m *Model) assign(windowID, session, taskID, projectID, status string) actionMsg {
	if taskID == "" && projectID == "" {
		return actionMsg{status: status}
	}
	if err := m.store.Assign(windowID, session, taskID, projectID); err != nil {
		return actionMsg{status: status + "; not assigned: " + err.Error()}
	}
	a, _ := m.assignmentOf(windowID)
	return actionMsg{status: status + " · " + a.path}
}

// pruneAssignments drops the assignments of windows gone from the listing.
// A window missing from one listing may have been made after that listing
// was asked for, so a window is pruned only when two listings in a row
// miss it.
func (m *Model) pruneAssignments(live map[string]bool) {
	missing := map[string]bool{}
	keep := map[string]bool{}
	for w := range m.store.Assignments() {
		if live[w] {
			keep[w] = true
			continue
		}
		if !m.missing[w] {
			missing[w] = true
			keep[w] = true
		}
	}
	m.missing = missing
	if err := m.store.Prune(keep); err != nil {
		m.setStatus("error: " + err.Error())
	}
}
