package ui

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/unixsuperhero/tuios-deck/internal/panes"
	"github.com/unixsuperhero/tuios-deck/internal/store"
)

// trow is a line of the tasks page: a project header, or a task at a depth
// under its project.
type trow struct {
	Project *store.Project
	Task    *store.Task
	Depth   int
}

func (r trow) id() string {
	if r.Project != nil {
		return r.Project.ID
	}
	if r.Task != nil {
		return r.Task.ID
	}
	return ""
}

func (r trow) name() string {
	if r.Project != nil {
		return "project " + r.Project.Name
	}
	return "task " + r.Task.Title
}

// rebuildTasks re-reads the store into the rows of the tasks page. The
// selection stays on the same project or task; when that row is gone it
// stays at the same place in the list.
func (m *Model) rebuildTasks() {
	var rows []trow
	for _, p := range m.store.Projects() {
		p := p
		rows = append(rows, trow{Project: &p})
		rows = m.appendTasks(rows, m.store.Roots(p.ID), 1)
	}
	m.trows = rows
	found := -1
	for i, r := range rows {
		if r.id() == m.tselID {
			found = i
			break
		}
	}
	if found < 0 {
		found = m.tsel
		if found >= len(rows) {
			found = len(rows) - 1
		}
		if found < 0 {
			found = 0
		}
	}
	m.tselect(found)
}

func (m *Model) appendTasks(rows []trow, tasks []store.Task, depth int) []trow {
	for _, t := range tasks {
		t := t
		rows = append(rows, trow{Task: &t, Depth: depth})
		rows = m.appendTasks(rows, m.store.Children(t.ID), depth+1)
	}
	return rows
}

func (m *Model) tselect(i int) {
	if i < 0 || i >= len(m.trows) {
		m.tsel, m.tselID = -1, ""
		return
	}
	m.tsel, m.tselID = i, m.trows[i].id()
}

func (m *Model) tmove(dir int) {
	if next := m.tsel + dir; next >= 0 && next < len(m.trows) {
		m.tselect(next)
	}
}

// selectedRow is the tasks page's selected row, or nil.
func (m *Model) selectedRow() *trow {
	if m.tsel >= 0 && m.tsel < len(m.trows) {
		return &m.trows[m.tsel]
	}
	return nil
}

// projectCounts is how many tasks a project holds at any depth, and how
// many of them are open, that is not done or skipped.
func (m *Model) projectCounts(projectID string) (total, open int) {
	for _, t := range m.store.Tasks() {
		if m.store.ProjectOf(t.ID) == projectID {
			total++
			if !store.IsFinal(t.Status) {
				open++
			}
		}
	}
	return total, open
}

// assignedPanes is every live pane, in list order, whose assignment is the
// task or, with no task id, resolves to the project.
func (m *Model) assignedPanes(projectID, taskID string) []*panes.Pane {
	var out []*panes.Pane
	for _, r := range m.rows {
		if r.Pane == nil {
			continue
		}
		project, task, _ := m.store.Resolve(r.Pane.ID)
		switch {
		case taskID != "":
			if task != nil && task.ID == taskID {
				out = append(out, r.Pane)
			}
		case project != nil && project.ID == projectID:
			out = append(out, r.Pane)
		}
	}
	return out
}

// subtree is a task and every task under it, by id.
func (m *Model) subtree(taskID string) map[string]bool {
	ids := map[string]bool{taskID: true}
	for _, c := range m.store.Children(taskID) {
		for id := range m.subtree(c.ID) {
			ids[id] = true
		}
	}
	return ids
}

// cascade counts what deleting the row takes with it: the tasks under it
// and the assignments that point at it or at any of them.
func (m *Model) cascade(r trow) (tasks, assigned int) {
	ids := map[string]bool{}
	if r.Project != nil {
		for _, t := range m.store.Tasks() {
			if m.store.ProjectOf(t.ID) == r.Project.ID {
				ids[t.ID] = true
			}
		}
	} else {
		ids = m.subtree(r.Task.ID)
		delete(ids, r.Task.ID)
	}
	for _, a := range m.store.Assignments() {
		if ids[a.TaskID] || (r.Project != nil && a.ProjectID == r.Project.ID) || (r.Task != nil && a.TaskID == r.Task.ID) {
			assigned++
		}
	}
	return len(ids), assigned
}

func plural(n int, word string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, word)
	}
	return fmt.Sprintf("%d %ss", n, word)
}

// nextStatus is the status after st in the order s cycles through: open,
// done, skipped, then open again.
func nextStatus(st string) string {
	for i, s := range store.Statuses {
		if s == st {
			return store.Statuses[(i+1)%len(store.Statuses)]
		}
	}
	return store.Statuses[0]
}

// statusMark is the mark drawn before a task's title: ✓ when done, ⊘ when
// skipped, a space when open.
func statusMark(st string) string {
	switch st {
	case "done":
		return "✓"
	case "skipped":
		return "⊘"
	}
	return " "
}

// nextColor is the palette colour after c, wrapping round.
func nextColor(c string) string {
	pal := store.Palette()
	for i, n := range pal {
		if n == c {
			return pal[(i+1)%len(pal)]
		}
	}
	return pal[0]
}

func (m *Model) handleTasksKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	r := m.selectedRow()
	switch msg.String() {
	case "esc":
		m.page = pageDeck
		return m, m.loadDetail(true)
	case "j", "down":
		m.tmove(1)
	case "k", "up":
		m.tmove(-1)
	case "g", "home":
		m.tselect(0)
	case "G", "end":
		m.tselect(len(m.trows) - 1)
	case "r":
		m.rebuildTasks()
		return m, m.refreshList()
	case "N":
		return m, m.openProjectForm()
	case "n":
		return m, m.openTaskForm()
	case "e":
		return m, m.openRenameForm()
	case "M":
		return m, m.openGlyphForm()
	case "s":
		if r == nil {
			return m, nil
		}
		if r.Project != nil {
			status := nextStatus(r.Project.Status)
			return m, m.storeChange(m.store.SetProjectStatus(r.Project.ID, status), r.Project.Name+" is "+status)
		}
		status := nextStatus(r.Task.Status)
		return m, m.storeChange(m.store.SetTaskStatus(r.Task.ID, status), r.Task.Title+" is "+status)
	case "m":
		if r == nil {
			return m, nil
		}
		if r.Project != nil {
			th := r.Project.Theme
			th.Color = nextColor(th.Color)
			return m, m.storeChange(m.store.SetProjectTheme(r.Project.ID, th), r.Project.Name+" is "+th.Color)
		}
		th := r.Task.Theme
		th.Color = nextColor(th.Color)
		return m, m.storeChange(m.store.SetTaskTheme(r.Task.ID, th), r.Task.Title+" is "+th.Color)
	case "x":
		if r == nil {
			return m, nil
		}
		tasks, assigned := m.cascade(*r)
		row := *r
		word := "subtask"
		if row.Project != nil {
			word = "task"
		}
		m.mode = modeConfirm
		m.confirm.Text = fmt.Sprintf("delete %s, %s and %s? (y/N)", row.name(), plural(tasks, word), plural(assigned, "assignment"))
		m.confirm.Do = func() tea.Msg {
			var err error
			if row.Project != nil {
				err = m.store.DeleteProject(row.Project.ID)
			} else {
				err = m.store.DeleteTask(row.Task.ID)
			}
			if err != nil {
				return actionMsg{err: err}
			}
			return actionMsg{status: "deleted " + row.name()}
		}
	case "enter":
		if r == nil {
			return m, nil
		}
		return m, m.jumpToPane(*r)
	case "a":
		return m, m.assignSelectedPane()
	}
	return m, nil
}

// storeChange reports a store call made on the spot: the status on
// success, the error otherwise, and the refresh either way.
func (m *Model) storeChange(err error, status string) tea.Cmd {
	if err != nil {
		m.setStatus("error: " + err.Error())
	} else {
		m.setStatus(status)
	}
	m.rebuildTasks()
	return m.refreshList()
}

// jumpToPane goes back to the deck page with the first pane assigned to
// the row selected.
func (m *Model) jumpToPane(r trow) tea.Cmd {
	var found []*panes.Pane
	if r.Project != nil {
		found = m.assignedPanes(r.Project.ID, "")
	} else {
		found = m.assignedPanes("", r.Task.ID)
	}
	if len(found) == 0 {
		m.setStatus("no pane is assigned to " + r.name())
		return nil
	}
	for i, row := range m.rows {
		if row.Pane == found[0] {
			m.sel = i
		}
	}
	m.page = pageDeck
	m.detail, m.detailErr, m.revision = nil, "", 0
	m.follow = true
	m.setStatus(found[0].Session + " › " + found[0].Name)
	return m.loadDetail(true)
}

// assignSelectedPane ties the pane selected on the deck page to the
// selected task or project.
func (m *Model) assignSelectedPane() tea.Cmd {
	r := m.selectedRow()
	if r == nil {
		return nil
	}
	p := m.selected()
	if p == nil {
		m.setStatus("select a pane on the deck page first")
		return nil
	}
	taskID, projectID := "", ""
	if r.Task != nil {
		taskID = r.Task.ID
	} else {
		projectID = r.Project.ID
	}
	err := m.store.Assign(p.ID, p.Session, taskID, projectID)
	a, _ := m.assignmentOf(p.ID)
	return m.storeChange(err, "assigned "+p.Session+" › "+p.Name+" to "+a.path)
}

func (m *Model) openProjectForm() tea.Cmd {
	m.mode = modeForm
	m.form = &form{
		Title:  "new project",
		Fields: []field{{Label: "name", Keep: true}},
		Validate: func(v []string) (int, string) {
			if v[0] == "" {
				return 0, "name is empty"
			}
			if m.store.ProjectByName(v[0]) != nil {
				return 0, "project " + v[0] + " exists"
			}
			return 0, ""
		},
		Done: func(v []string) tea.Cmd {
			return func() tea.Msg {
				p, err := m.store.AddProject(v[0], nil)
				if err != nil {
					return actionMsg{err: err}
				}
				return actionMsg{status: "added project " + p.Name, selectRow: p.ID}
			}
		},
	}
	return nil
}

// openTaskForm adds a top-level task to the selected project, or a subtask
// of the selected task.
func (m *Model) openTaskForm() tea.Cmd {
	r := m.selectedRow()
	if r == nil {
		m.setStatus("add a project first (N)")
		return nil
	}
	projectID, parentID, title := "", "", ""
	if r.Project != nil {
		projectID, title = r.Project.ID, "new task in "+r.Project.Name
	} else {
		parentID, title = r.Task.ID, "new subtask of "+r.Task.Title
	}
	m.mode = modeForm
	m.form = &form{
		Title:  title,
		Fields: []field{{Label: "title", Keep: true}},
		Validate: func(v []string) (int, string) {
			if v[0] == "" {
				return 0, "title is empty"
			}
			return 0, ""
		},
		Done: func(v []string) tea.Cmd {
			return func() tea.Msg {
				t, err := m.store.AddTask(projectID, parentID, v[0], nil)
				if err != nil {
					return actionMsg{err: err}
				}
				return actionMsg{status: "added " + t.Title, selectRow: t.ID}
			}
		},
	}
	return nil
}

func (m *Model) openRenameForm() tea.Cmd {
	r := m.selectedRow()
	if r == nil {
		return nil
	}
	row := *r
	label, current := "title", ""
	if row.Project != nil {
		label, current = "name", row.Project.Name
	} else {
		current = row.Task.Title
	}
	m.mode = modeForm
	m.form = &form{
		Title:  "rename " + row.name(),
		Fields: []field{{Label: label, Default: current}},
		Validate: func(v []string) (int, string) {
			if row.Project != nil {
				if p := m.store.ProjectByName(v[0]); p != nil && p.ID != row.Project.ID {
					return 0, "project " + v[0] + " exists"
				}
			}
			return 0, ""
		},
		Done: func(v []string) tea.Cmd {
			return func() tea.Msg {
				var err error
				if row.Project != nil {
					err = m.store.RenameProject(row.Project.ID, v[0])
				} else {
					err = m.store.RenameTask(row.Task.ID, v[0])
				}
				if err != nil {
					return actionMsg{err: err}
				}
				return actionMsg{status: "renamed to " + v[0]}
			}
		},
	}
	return nil
}

// openGlyphForm sets the glyph of the selected row's theme: one grapheme.
func (m *Model) openGlyphForm() tea.Cmd {
	r := m.selectedRow()
	if r == nil {
		return nil
	}
	row := *r
	th := row.Task.Theme
	if row.Project != nil {
		th = row.Project.Theme
	}
	m.mode = modeForm
	m.form = &form{
		Title:  "glyph for " + row.name(),
		Fields: []field{{Label: "glyph", Default: th.Glyph}},
		Validate: func(v []string) (int, string) {
			if v[0] == "" {
				return 0, "glyph is empty"
			}
			if first, _ := ansi.FirstGraphemeCluster(v[0], ansi.GraphemeWidth); first != v[0] {
				return 0, "one character only"
			}
			return 0, ""
		},
		Done: func(v []string) tea.Cmd {
			th.Glyph = v[0]
			return func() tea.Msg {
				var err error
				if row.Project != nil {
					err = m.store.SetProjectTheme(row.Project.ID, th)
				} else {
					err = m.store.SetTaskTheme(row.Task.ID, th)
				}
				if err != nil {
					return actionMsg{err: err}
				}
				return actionMsg{status: row.name() + " is now " + v[0]}
			}
		},
	}
	return nil
}

// taskListLines draws the tree of the tasks page.
func (m *Model) taskListLines(width, height int) []string {
	lines := make([]string, 0, height)
	if len(m.trows) == 0 {
		lines = append(lines, cDim.Render("no projects: press N"))
	}
	start := 0
	if m.tsel >= height {
		start = m.tsel - height + 1
	}
	for i := start; i < len(m.trows) && len(lines) < height; i++ {
		r := m.trows[i]
		var line string
		if r.Project != nil {
			_, open := m.projectCounts(r.Project.ID)
			text := fmt.Sprintf(" %d open", open)
			name := cTitle
			if store.IsFinal(r.Project.Status) {
				text = " " + statusMark(r.Project.Status) + " " + r.Project.Status + " ·" + text
				name = cDim
			}
			count := cDim.Render(text)
			glyph := themeStyle(r.Project.Theme).Render(r.Project.Theme.Glyph)
			line = glyph + " " + name.Render(ansi.Truncate(r.Project.Name, width-2-ansi.StringWidth(count), "…")) + count
		} else {
			t := r.Task
			indent := strings.Repeat("  ", r.Depth)
			glyph := themeStyle(t.Theme).Render(t.Theme.Glyph)
			mark, title := " ", t.Title
			if store.IsFinal(t.Status) {
				mark, title = cDim.Render(statusMark(t.Status)), cDim.Render(t.Title)
			}
			count := ""
			if n := len(m.assignedPanes("", t.ID)); n > 0 {
				count = cDim.Render(plural(n, "pane"))
			}
			room := width - len(indent) - 4 - ansi.StringWidth(count) - 1
			if room < 1 {
				room = 1
			}
			title = ansi.Truncate(title, room, "…")
			fill := width - len(indent) - 4 - ansi.StringWidth(title) - ansi.StringWidth(count)
			if fill < 1 {
				fill = 1
			}
			line = indent + glyph + " " + mark + " " + title + strings.Repeat(" ", fill) + count
		}
		if i == m.tsel {
			line = cSel.Render(ansi.Strip(pad(line, width)))
		}
		lines = append(lines, line)
	}
	for len(lines) < height {
		lines = append(lines, "")
	}
	return lines
}

// taskDetailLines describes the selected project or task on the right.
func (m *Model) taskDetailLines(width, height int) []string {
	var lines []string
	field := func(label, value string) {
		lines = append(lines, cDim.Render(pad(label, 9))+value)
	}
	paneLines := func(ps []*panes.Pane) {
		field("panes", fmt.Sprintf("%d", len(ps)))
		for _, p := range ps {
			state := p.State
			if !p.IsAgent {
				state = p.Fg
			}
			line := "  " + p.Session + " › " + p.Name + "  " + stateStyle(p.State).Render(state)
			if _, task, _ := m.store.Resolve(p.ID); task != nil {
				line += "  " + cDim.Render(task.Title)
			}
			lines = append(lines, line)
		}
	}
	r := m.selectedRow()
	switch {
	case r == nil:
		lines = append(lines, cDim.Render("no project selected"))
	case r.Project != nil:
		p := r.Project
		total, open := m.projectCounts(p.ID)
		lines = append(lines, themeStyle(p.Theme).Render(p.Theme.Glyph)+" "+cTitle.Render(p.Name))
		field("status", p.Status)
		field("theme", p.Theme.Color+" "+p.Theme.Glyph)
		field("tasks", fmt.Sprintf("%d (%d open)", total, open))
		field("created", time.Unix(p.Created, 0).Format("2006-01-02 15:04"))
		paneLines(m.assignedPanes(p.ID, ""))
	default:
		t := r.Task
		lines = append(lines, themeStyle(t.Theme).Render(t.Theme.Glyph)+" "+cTitle.Render(t.Title))
		path := "?"
		if p := m.store.Project(m.store.ProjectOf(t.ID)); p != nil {
			path = p.Name
		}
		for _, c := range m.store.Path(t.ID) {
			path += " › " + c.Title
		}
		field("path", path)
		field("status", t.Status)
		field("theme", t.Theme.Color+" "+t.Theme.Glyph)
		field("created", time.Unix(t.Created, 0).Format("2006-01-02 15:04"))
		field("updated", time.Unix(t.Updated, 0).Format("2006-01-02 15:04"))
		children := m.store.Children(t.ID)
		field("subtasks", fmt.Sprintf("%d", len(children)))
		for _, c := range children {
			mark := statusMark(c.Status)
			lines = append(lines, "  "+themeStyle(c.Theme).Render(c.Theme.Glyph)+" "+mark+" "+c.Title)
		}
		paneLines(m.assignedPanes("", t.ID))
	}
	for i := range lines {
		lines[i] = ansi.Truncate(lines[i], width, "…")
	}
	if len(lines) > height {
		lines = lines[:height]
	}
	for len(lines) < height {
		lines = append(lines, "")
	}
	return lines
}
