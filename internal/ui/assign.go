package ui

import (
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

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
// task starts as the active task and the project defaults to the active
// project, else to the selected pane's project. Focus prefills editable
// values so creation works with an empty scope and explicit choices still win.
func (m *Model) targetFields(keep bool) []field {
	project, task := "", ""
	if a := m.store.Active(); a.ProjectID != "" {
		if pr := m.store.Project(a.ProjectID); pr != nil {
			project = pr.Name
		}
		if t := m.store.Task(a.TaskID); t != nil {
			task = m.taskPath(*t)
		}
	}
	if p := m.selected(); p != nil && project == "" {
		if pr, _, _ := m.store.Resolve(p.ID); pr != nil {
			project = pr.Name
		}
	}
	fields := []field{
		{Label: "task", Keep: true, Value: task, Options: m.taskOptions, Preview: m.taskPreview, New: "task"},
		{Label: "project", Default: project, Keep: keep, Options: m.projectOptions, Preview: m.projectPreview, New: "project"},
	}
	if m.focus != nil {
		fields[0].Value = ""
		fields[1].Default = ""
		if t := m.store.Task(m.focus.taskID); t != nil {
			fields[0].Value = m.taskPath(*t)
		} else if p := m.store.Project(m.focus.projectID); p != nil {
			fields[1].Value = p.Name
		}
	}
	return fields
}

// autofillTaskProject runs after form updates, including prefills and returning
// from mass-add. Only a changed, resolved task updates the following project;
// empty or invalid task text leaves the project editable and validation alone.
func (m *Model) autofillTaskProject() {
	f := m.form
	if m.mode != modeForm || f == nil {
		return
	}
	for i := 0; i+1 < len(f.Fields); i++ {
		task, project := &f.Fields[i], &f.Fields[i+1]
		if task.Label != "task" || task.New != "task" || project.Label != "project" || project.New != "project" {
			continue
		}
		value := task.result()
		if value == f.targetTask {
			return
		}
		f.targetTask = value
		t, err := m.findTask(value)
		if err != "" {
			return
		}
		project.Value, project.Default, project.hl = "", "", 0
		if p := m.store.Project(m.store.ProjectOf(t.ID)); p != nil {
			project.Value = p.Name
		}
		return
	}
}

// assignment is the theme and the text a pane's assignment is drawn with:
// the task's, or the project's when the pane has only a project.
type assignment struct {
	glyph string
	style lipgloss.Style
	path  string // "project › top task › task"
	// The rail row draws the project's first four letters and the task
	// title, each in its own theme colour.
	chip      string
	chipStyle lipgloss.Style
	task      string
	taskStyle lipgloss.Style
}

func (m *Model) assignmentOf(windowID string) (assignment, bool) {
	project, task, _ := m.store.Resolve(windowID)
	if project == nil && task == nil {
		return assignment{}, false
	}
	var a assignment
	if project != nil {
		a.glyph, a.style = project.Theme.Glyph, themeStyle(project.Theme)
		a.path = project.Name
		a.chip, a.chipStyle = project.Name, a.style
	}
	if task != nil {
		a.glyph, a.style = task.Theme.Glyph, themeStyle(task.Theme)
		a.task, a.taskStyle = task.Title, a.style
		for _, c := range m.store.Path(task.ID) {
			a.path += " › " + c.Title
		}
	}
	if a.glyph == "" {
		a.glyph = "●"
	}
	if a.chip == "" {
		a.chip = "?"
	}
	a.chip = pad(ansi.Truncate(a.chip, 4, ""), 4)
	return a, true
}

func themeStyle(t store.Theme) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(lipgloss.Color(t.ANSI()))
}

// openAssign assigns, or reassigns, the selected pane to a task or a
// project through the tree.
func (m *Model) openAssign() tea.Cmd {
	p := m.selected()
	if p == nil {
		m.setStatus("select a pane first")
		return nil
	}
	run := protoRun{purpose: purposeAssign, title: "assign " + p.Name, windowID: p.ID, session: p.Session, name: p.Name}
	projectID, taskID := "", ""
	if a, ok := m.store.Assignment(p.ID); ok {
		projectID, taskID = a.ProjectID, a.TaskID
	}
	return m.openTreeView(run, projectID, taskID)
}

// assignPane ties the pane to the task or the project, or with neither
// drops its assignment.
func (m *Model) assignPane(windowID, session, name, taskID, projectID string) tea.Cmd {
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
