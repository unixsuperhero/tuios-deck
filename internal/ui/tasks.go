package ui

import (
	"fmt"
	"sort"
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
	if m.focus != nil {
		kept := rows[:0]
		for _, r := range rows {
			if r.Task != nil && m.focusTask(r.Task.ID) || r.Project != nil && m.focus.taskID == "" && r.Project.ID == m.focus.projectID {
				kept = append(kept, r)
			}
		}
		rows = kept
	}
	if m.easyOnly {
		kept := rows[:0]
		for _, r := range rows {
			if r.Task != nil && r.Task.Easy {
				r.Depth = 0
				kept = append(kept, r)
			}
		}
		rows = kept
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

// nextColor is the colour after c in a cycle that runs through the free
// colours first, then the rest of the palette, and wraps round.
func nextColor(c string, free []string) string {
	order := append([]string{}, free...)
	isFree := map[string]bool{}
	for _, f := range free {
		isFree[f] = true
	}
	for _, n := range store.Palette() {
		if !isFree[n] {
			order = append(order, n)
		}
	}
	for i, n := range order {
		if n == c {
			return order[(i+1)%len(order)]
		}
	}
	return order[0]
}

func (m *Model) handleTasksKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	r := m.selectedRow()
	switch msg.String() {
	case "R":
		m.easyOnly = false
		m.rebuildTasks()
		m.setStatus("task view reset; saved easy marks and focus unchanged")
	case "E":
		if r != nil && r.Task != nil {
			return m, m.storeChange(m.store.SetTaskEasy(r.Task.ID, !r.Task.Easy), "updated saved easy list")
		}
	case "esc":
		m.page = pageDeck
		return m, m.loadDetail(true)
	case "j", "down":
		m.tmove(1)
	case "k", "up":
		m.tmove(-1)
	case "{", "}":
		m.taskJump(map[string]int{"{": -1, "}": 1}[msg.String()])
	case "(", ")":
		m.taskNest(map[string]int{"(": -1, ")": 1}[msg.String()])
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
		return m, m.openEditForm()
	case "M":
		return m, m.openGlyphTable()
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
		free := m.store.FreeColors(r.id())
		if r.Project != nil {
			th := r.Project.Theme
			th.Color = nextColor(th.Color, free)
			return m, m.storeChange(m.store.SetProjectTheme(r.Project.ID, th), r.Project.Name+" is "+th.Color)
		}
		th := r.Task.Theme
		th.Color = nextColor(th.Color, free)
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
		m.confirm.Text = fmt.Sprintf("delete %s, %s and %s? (Y/n)", row.name(), plural(tasks, word), plural(assigned, "assignment"))
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

// openTaskForm adds a task under the selected row: a top-level task of the
// selected project, or a subtask of the selected task.
func (m *Model) openTaskForm() tea.Cmd {
	r := m.selectedRow()
	if r == nil {
		m.setStatus("add a project first (N)")
		return nil
	}
	if r.Project != nil {
		return m.openNewTask(topLevel(r.Project.Name))
	}
	return m.openNewTask(m.taskPath(*r.Task))
}

// openNewTask opens the new-task form: the parent, starting at parent,
// then the title. ctrl+h walks the parent up a level from any field, to the
// project's top level at most, and ctrl+l walks back down the way it came.
func (m *Model) openNewTask(parent string) tea.Cmd {
	f := &form{
		Fields: []field{
			{Label: "parent", Default: parent, Options: func() []string { return m.parentOptions(nil) }, Preview: m.parentPreview, New: "task"},
			{Label: "title", Keep: true},
		},
		Validate: func(v []string) (int, string) {
			if _, _, msg := m.resolveParent(v[0]); msg != "" {
				return 0, msg
			}
			if v[1] == "" {
				return 1, "title is empty"
			}
			return 0, ""
		},
		Done: func(v []string) tea.Cmd {
			parentID, projectID, _ := m.resolveParent(v[0])
			return func() tea.Msg {
				t, err := m.store.AddTask(projectID, parentID, v[1], nil)
				if err != nil {
					return actionMsg{err: err}
				}
				return actionMsg{status: "added " + m.taskPath(t), selectRow: t.ID}
			}
		},
	}
	hint := func() string { return m.parentHint(f.Fields[0].result()) }
	f.Fields[0].HintFn, f.Fields[1].HintFn = hint, hint
	f.Title = m.newTaskTitle(parent)
	if parent != "" {
		f.Cur = 1
	}
	var down []string // the entries walked up from, nearest last
	f.OnKey = func(key string) bool {
		cur := f.Fields[0].result()
		switch key {
		case "ctrl+h":
			up := m.parentUp(cur)
			if up == "" {
				return true
			}
			down = append(down, cur)
			f.Fields[0].Value = up
		case "ctrl+l":
			if len(down) == 0 || m.parentUp(down[len(down)-1]) != cur {
				down = nil
				return true
			}
			f.Fields[0].Value, down = down[len(down)-1], down[:len(down)-1]
		default:
			return false
		}
		f.Fields[0].hl = 0
		f.Title = m.newTaskTitle(f.Fields[0].Value)
		return true
	}
	m.mode, m.form = modeForm, f
	return nil
}

// parentUp is the parent entry one level above entry: the parent task's
// path, the project's top level for a top-level task, and "" from a
// project's top level or an entry that names nothing.
func (m *Model) parentUp(entry string) string {
	parentID, _, msg := m.resolveParent(entry)
	if msg != "" || parentID == "" {
		return ""
	}
	return m.parentEntry(*m.store.Task(parentID))
}

// parentHint names where the new task goes, by its full path, and the keys
// that walk it.
func (m *Model) parentHint(entry string) string {
	keys := " · ctrl+h parent up · ctrl+l back down"
	parentID, projectID, msg := m.resolveParent(entry)
	switch {
	case msg != "":
		return msg + keys
	case parentID != "":
		path := "?"
		if p := m.store.Project(m.store.ProjectOf(parentID)); p != nil {
			path = p.Name
		}
		for _, c := range m.store.Path(parentID) {
			path += " › " + c.Title
		}
		return "under " + path + keys
	}
	return "at the top level of " + m.store.Project(projectID).Name + keys
}

// newTaskTitle is the new-task form's title for a parent entry.
func (m *Model) newTaskTitle(entry string) string {
	parentID, projectID, msg := m.resolveParent(entry)
	switch {
	case msg != "":
		return "new task"
	case parentID != "":
		return "new subtask of " + m.store.Task(parentID).Title
	}
	return "new task in " + m.store.Project(projectID).Name
}

// topLevel is the parent entry that stands for a project's top level.
func topLevel(project string) string { return "(top level of " + project + ")" }

// parentOptions is a top-level entry for every project, then every task's
// path but those in skip, for a parent field.
func (m *Model) parentOptions(skip map[string]bool) []string {
	var out []string
	for _, p := range m.store.Projects() {
		out = append(out, topLevel(p.Name))
	}
	var paths []string
	for _, t := range m.store.Tasks() {
		if !skip[t.ID] {
			paths = append(paths, m.taskPath(t))
		}
	}
	sort.Strings(paths)
	return append(out, paths...)
}

// parentEntry is the parent field's text for where a task sits: its
// parent's path, or its project's top-level entry.
func (m *Model) parentEntry(t store.Task) string {
	if t.ParentID != "" {
		if p := m.store.Task(t.ParentID); p != nil {
			return m.taskPath(*p)
		}
	}
	if p := m.store.Project(m.store.ProjectOf(t.ID)); p != nil {
		return topLevel(p.Name)
	}
	return ""
}

// resolveParent turns a parent field's text into a parent task id, or a
// project id for a project's top level.
func (m *Model) resolveParent(text string) (parentID, projectID, msg string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return "", "", "a parent is required (tab lists them)"
	}
	low := strings.ToLower(text)
	if strings.HasPrefix(low, "(top level of ") && strings.HasSuffix(low, ")") {
		name := text[len("(top level of ") : len(text)-1]
		p := m.store.ProjectByName(name)
		if p == nil {
			return "", "", "no project " + name
		}
		return "", p.ID, ""
	}
	t, msg := m.findTask(text)
	if msg != "" {
		return "", "", msg
	}
	return t.ID, "", ""
}

// colorOptions is the palette with the colours free for the row first.
func (m *Model) colorOptions(id string) func() []string {
	return func() []string {
		free := m.store.FreeColors(id)
		out := append([]string{}, free...)
		isFree := map[string]bool{}
		for _, c := range free {
			isFree[c] = true
		}
		for _, c := range store.Palette() {
			if !isFree[c] {
				out = append(out, c)
			}
		}
		return out
	}
}

func statusOptions() []string { return store.Statuses }

// checkTheme refuses a colour outside the palette or a glyph that is not
// one character, naming the field at colourField or colourField+1.
func checkTheme(color, glyph string, colourField int) (int, string) {
	if colorIndexOf(color) < 0 {
		return colourField, "no colour " + color + " (tab lists them)"
	}
	if glyph == "" {
		return colourField + 1, "glyph is empty"
	}
	if first, _ := ansi.FirstGraphemeCluster(glyph, ansi.GraphemeWidth); first != glyph {
		return colourField + 1, "one character only"
	}
	return 0, ""
}

func colorIndexOf(name string) int {
	for i, c := range store.Palette() {
		if c == name {
			return i
		}
	}
	return -1
}

func checkStatusField(st string, i int) (int, string) {
	for _, s := range store.Statuses {
		if s == st {
			return 0, ""
		}
	}
	return i, "status is open, done or skipped"
}

// openEditForm edits the selected row in one form: a task's title, parent,
// status, colour and glyph, or a project's name, status, colour and glyph.
// Only what changed is written, and the status says what that was.
func (m *Model) openEditForm() tea.Cmd {
	r := m.selectedRow()
	if r == nil {
		return nil
	}
	if r.Project != nil {
		return m.openProjectEditForm(*r.Project)
	}
	t := *r.Task
	skip := m.subtree(t.ID)
	parent := m.parentEntry(t)
	m.mode = modeForm
	m.form = &form{
		Title: "edit task " + t.Title,
		Fields: []field{
			{Label: "title", Default: t.Title},
			{Label: "parent", Default: parent, Options: func() []string { return m.parentOptions(skip) }, Preview: m.parentPreview, New: "task"},
			{Label: "status", Default: t.Status, Options: statusOptions},
			{Label: "colour", Default: t.Theme.Color, Options: m.colorOptions(t.ID)},
			{Label: "glyph", Default: t.Theme.Glyph},
		},
		Validate: func(v []string) (int, string) {
			parentID, _, msg := m.resolveParent(v[1])
			if msg != "" {
				return 1, msg
			}
			if skip[parentID] {
				return 1, "a task cannot move under itself or one of its subtasks"
			}
			if i, msg := checkStatusField(v[2], 2); msg != "" {
				return i, msg
			}
			return checkTheme(v[3], v[4], 3)
		},
		Done: func(v []string) tea.Cmd {
			parentID, projectID, _ := m.resolveParent(v[1])
			return func() tea.Msg {
				var did []string
				if v[0] != t.Title {
					if err := m.store.RenameTask(t.ID, v[0]); err != nil {
						return actionMsg{err: err}
					}
					did = append(did, "renamed to "+v[0])
				}
				if v[1] != parent {
					if err := m.store.MoveTask(t.ID, parentID, projectID); err != nil {
						return actionMsg{err: err}
					}
					if parentID != "" {
						did = append(did, "moved under "+m.taskPath(*m.store.Task(parentID)))
					} else {
						did = append(did, "moved to the top level of "+m.store.Project(projectID).Name)
					}
				}
				if v[2] != t.Status {
					if err := m.store.SetTaskStatus(t.ID, v[2]); err != nil {
						return actionMsg{err: err}
					}
					did = append(did, "status "+v[2])
				}
				if th := (store.Theme{Color: v[3], Glyph: v[4]}); th != t.Theme {
					if err := m.store.SetTaskTheme(t.ID, th); err != nil {
						return actionMsg{err: err}
					}
					did = append(did, themeChange(t.Theme, th)...)
				}
				return actionMsg{status: edited(v[0], did), selectRow: t.ID}
			}
		},
	}
	m.glyphField(m.form, 4, *r, 3, 0)
	return nil
}

func (m *Model) openProjectEditForm(p store.Project) tea.Cmd {
	m.mode = modeForm
	m.form = &form{
		Title: "edit project " + p.Name,
		Fields: []field{
			{Label: "name", Default: p.Name},
			{Label: "status", Default: p.Status, Options: statusOptions},
			{Label: "colour", Default: p.Theme.Color, Options: m.colorOptions(p.ID)},
			{Label: "glyph", Default: p.Theme.Glyph},
		},
		Validate: func(v []string) (int, string) {
			if o := m.store.ProjectByName(v[0]); o != nil && o.ID != p.ID {
				return 0, "project " + v[0] + " exists"
			}
			if i, msg := checkStatusField(v[1], 1); msg != "" {
				return i, msg
			}
			return checkTheme(v[2], v[3], 2)
		},
		Done: func(v []string) tea.Cmd {
			return func() tea.Msg {
				var did []string
				if v[0] != p.Name {
					if err := m.store.RenameProject(p.ID, v[0]); err != nil {
						return actionMsg{err: err}
					}
					did = append(did, "renamed to "+v[0])
				}
				if v[1] != p.Status {
					if err := m.store.SetProjectStatus(p.ID, v[1]); err != nil {
						return actionMsg{err: err}
					}
					did = append(did, "status "+v[1])
				}
				if th := (store.Theme{Color: v[2], Glyph: v[3]}); th != p.Theme {
					if err := m.store.SetProjectTheme(p.ID, th); err != nil {
						return actionMsg{err: err}
					}
					did = append(did, themeChange(p.Theme, th)...)
				}
				return actionMsg{status: edited(v[0], did), selectRow: p.ID}
			}
		},
	}
	m.glyphField(m.form, 3, trow{Project: &p}, 2, 0)
	return nil
}

func themeChange(was, now store.Theme) []string {
	var did []string
	if now.Color != was.Color {
		did = append(did, "colour "+now.Color)
	}
	if now.Glyph != was.Glyph {
		did = append(did, "glyph "+now.Glyph)
	}
	return did
}

// edited is the status after an edit: what changed, or that nothing did.
func edited(name string, did []string) string {
	if len(did) == 0 {
		return name + ": nothing changed"
	}
	return name + ": " + strings.Join(did, ", ")
}

// taskListLines draws the tree of the tasks page.
func (m *Model) taskListLines(width, height int) []string {
	lines := make([]string, 0, height)
	if len(m.trows) == 0 {
		text := "no projects: press N"
		if m.easyOnly {
			text = "no saved easy tasks: ctrl+f all, E mark easy"
		}
		lines = append(lines, cDim.Render(text))
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
			if t.Easy {
				title += " [easy]"
			}
			if store.IsFinal(t.Status) {
				mark, title = cDim.Render(statusMark(t.Status)), cDim.Render(title)
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
		if m.focus == nil {
			lines = append(lines, m.noteLines(r)...)
		}
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
		if m.focus == nil {
			lines = append(lines, m.noteLines(r)...)
		}
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
