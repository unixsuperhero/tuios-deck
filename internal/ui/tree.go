package ui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/unixsuperhero/tuios-deck/internal/panes"
	"github.com/unixsuperhero/tuios-deck/internal/store"
)

// treeModal is the task tree modal over the deck: the whole project tree
// of the selected pane, or of the first assigned pane under the selected
// group header, with a row per project, task and subtask. It keeps the
// pane it was opened from, so the tree follows the pane's assignment, and
// it stays open across the forms and confirmations its keys start: while
// m.tree is set, modeNormal on the deck page shows it again.
type treeModal struct {
	paneID    string // the pane it was opened from; empty from a header
	header    string // the header it was opened from
	projectID string // the project shown when no pane names one
	sel       int
	selID     string
}

// openTree opens the task tree modal for the selected rail row. The rail
// selection and the detail are left as they are.
func (m *Model) openTree() tea.Cmd {
	if m.sel < 0 || m.sel >= len(m.rows) {
		m.setStatus("select a pane or a group first")
		return nil
	}
	r := m.rows[m.sel]
	t := &treeModal{}
	if r.Pane != nil {
		t.paneID = r.Pane.ID
	} else {
		t.header = r.Header
		for i := m.sel + 1; i < len(m.rows); i++ {
			if m.rows[i].Header != "" && m.rows[i].Depth <= r.Depth {
				break
			}
			if p := m.rows[i].Pane; p != nil {
				if pr, _, _ := m.store.Resolve(p.ID); pr != nil {
					t.projectID = pr.ID
					break
				}
			}
		}
	}
	m.tree = t
	t.selID = m.treeNode()
	m.mode = modeTree
	return nil
}

// reshowTree puts the modal back once a form, a picker or a confirmation
// it started has closed.
func (m *Model) reshowTree() {
	if m.tree != nil && m.mode == modeNormal && m.page == pageDeck && m.live == nil {
		m.mode = modeTree
	}
}

// closeTree closes the modal.
func (m *Model) closeTree() {
	m.tree = nil
	if m.mode == modeTree {
		m.mode = modeNormal
	}
}

// treePane is the pane the modal was opened from, while it is listed.
func (m *Model) treePane() *panes.Pane {
	if m.tree == nil || m.tree.paneID == "" {
		return nil
	}
	for i := range m.merged {
		for j := range m.merged[i].Panes {
			if p := &m.merged[i].Panes[j]; p.ID == m.tree.paneID {
				return p
			}
		}
	}
	return nil
}

// treeNode is the id of the pane's own node: its task, else its project.
func (m *Model) treeNode() string {
	if m.tree.paneID == "" {
		return ""
	}
	pr, t, _ := m.store.Resolve(m.tree.paneID)
	switch {
	case t != nil:
		return t.ID
	case pr != nil:
		return pr.ID
	}
	return ""
}

// treeProject is the project the modal shows: the pane's, or the one the
// header's panes named.
func (m *Model) treeProject() *store.Project {
	if m.tree.paneID != "" {
		pr, _, _ := m.store.Resolve(m.tree.paneID)
		return pr
	}
	return m.store.Project(m.tree.projectID)
}

// treeModalRows is the project and every task under it, read from the store on
// each call so a change made from the modal shows at once.
func (m *Model) treeModalRows() []trow {
	p := m.treeProject()
	if p == nil {
		return nil
	}
	pc := *p
	return m.appendTasks([]trow{{Project: &pc}}, m.store.Roots(p.ID), 1)
}

// settleTree keeps the highlight on its node, else at its place.
func (m *Model) settleTree() []trow {
	rows := m.treeModalRows()
	for i, r := range rows {
		if r.id() == m.tree.selID {
			m.tree.sel = i
		}
	}
	if m.tree.sel >= len(rows) {
		m.tree.sel = len(rows) - 1
	}
	if m.tree.sel < 0 {
		m.tree.sel = 0
	}
	if m.tree.sel < len(rows) {
		m.tree.selID = rows[m.tree.sel].id()
	}
	return rows
}

const treeKeys = "j/k move · s status · e edit · n add under · x delete · a assign · enter tasks page · esc close"

// treeBox draws the modal as a box over the body.
func (m *Model) treeBox(width, maxH int) []string {
	iw := width - 4
	rows := m.settleTree()
	title := "task tree"
	if p := m.treeProject(); p != nil {
		title += " · " + p.Name
	}
	from := ""
	if p := m.treePane(); p != nil {
		from = "from " + p.Session + " › " + p.Name
	} else if m.tree.header != "" {
		from = "from group " + m.tree.header
	}
	lines := []string{boxTop(title, width)}
	if from != "" {
		lines = append(lines, boxRow(cDim.Render(ansi.Truncate(from, iw, "…")), iw))
	}
	if len(rows) == 0 {
		text := "no pane under this group is assigned to a project"
		if m.tree.paneID != "" {
			text = "this pane is not assigned to a task or project · a assigns it"
		}
		lines = append(lines, boxRow(cDim.Render(ansi.Truncate(text, iw, "…")), iw))
	}
	avail := maxH - len(lines) - 3
	if avail < 1 {
		avail = 1
	}
	start := 0
	if m.tree.sel >= avail {
		start = m.tree.sel - avail + 1
	}
	node := m.treeNode()
	for i := start; i < len(rows) && i < start+avail; i++ {
		l := m.treeLine(rows[i], iw-2, rows[i].id() == node)
		if i == m.tree.sel {
			l = cSel.Render(pad("▸ "+ansi.Strip(l), iw))
		} else {
			l = "  " + l
		}
		lines = append(lines, boxRow(l, iw))
	}
	return append(lines,
		cBorder.Render("├"+strings.Repeat("─", width-2)+"┤"),
		boxRow(cDim.Render(ansi.Truncate(treeKeys, iw, "…")), iw),
		cBorder.Render("╰"+strings.Repeat("─", width-2)+"╯"),
	)
}

// treeLine is one row of the modal: the indent, the glyph, the status mark,
// the name, then the panes assigned to it and the mark of the pane's node.
func (m *Model) treeLine(r trow, width int, own bool) string {
	var indent, glyph, mark, name string
	var n int
	if r.Project != nil {
		glyph = themeStyle(r.Project.Theme).Render(r.Project.Theme.Glyph)
		mark, name = statusMark(r.Project.Status), r.Project.Name
		n = len(m.assignedPanes(r.Project.ID, ""))
	} else {
		indent = strings.Repeat("  ", r.Depth)
		glyph = themeStyle(r.Task.Theme).Render(r.Task.Theme.Glyph)
		mark, name = statusMark(r.Task.Status), r.Task.Title
		n = len(m.assignedPanes("", r.Task.ID))
	}
	tail := ""
	if n > 0 {
		tail = plural(n, "pane")
	}
	if own {
		tail = strings.TrimSpace(tail + " ◂ this pane")
	}
	room := width - ansi.StringWidth(indent) - 4 - ansi.StringWidth(tail) - 1
	if room < 1 {
		room = 1
	}
	name = ansi.Truncate(name, room, "…")
	if r.Project != nil {
		name = cTitle.Render(name)
	}
	fill := width - ansi.StringWidth(indent) - 4 - ansi.StringWidth(ansi.Strip(name)) - ansi.StringWidth(tail)
	if fill < 1 {
		fill = 1
	}
	return indent + glyph + " " + cDim.Render(mark) + " " + name + strings.Repeat(" ", fill) + cDim.Render(tail)
}

// handleTreeKey answers the modal's keys. Each one acts on the highlighted
// node through the same calls the tasks page makes.
func (m *Model) handleTreeKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	rows := m.settleTree()
	var r *trow
	if m.tree.sel < len(rows) {
		r = &rows[m.tree.sel]
	}
	switch msg.String() {
	case "esc", "q", "ctrl+c":
		m.closeTree()
	case "?":
		return m, m.openHelp()
	case "j", "down", "ctrl+n":
		if m.tree.sel+1 < len(rows) {
			m.tree.sel++
			m.tree.selID = rows[m.tree.sel].id()
		}
	case "k", "up", "ctrl+p":
		if m.tree.sel > 0 {
			m.tree.sel--
			m.tree.selID = rows[m.tree.sel].id()
		}
	case "g", "home":
		if len(rows) > 0 {
			m.tree.sel, m.tree.selID = 0, rows[0].id()
		}
	case "G", "end":
		if len(rows) > 0 {
			m.tree.sel, m.tree.selID = len(rows)-1, rows[len(rows)-1].id()
		}
	case "s":
		if r != nil {
			return m, m.cycleRowStatus(*r)
		}
	case "e":
		if r != nil {
			m.mode = modeNormal
			return m, m.editRow(*r)
		}
	case "n":
		if r != nil {
			parent := ""
			if r.Project != nil {
				parent = r.Project.Name
			} else {
				parent = m.taskPath(*r.Task)
			}
			m.mode = modeNormal
			return m, m.openMassAdd(parent + "\n  ")
		}
	case "x":
		if r != nil {
			m.confirmDeleteRow(*r, modeTree)
		}
	case "a":
		p := m.treePane()
		if p == nil {
			m.setStatus("select a pane, not a group, to assign it")
			return m, nil
		}
		if r == nil {
			m.mode = modeNormal
			return m, m.openAssign()
		}
		taskID, projectID := "", ""
		if r.Task != nil {
			taskID = r.Task.ID
		} else {
			projectID = r.Project.ID
		}
		err := m.store.Assign(p.ID, p.Session, taskID, projectID)
		a, _ := m.assignmentOf(p.ID)
		return m, m.storeChange(err, "assigned "+p.Session+" › "+p.Name+" to "+a.path)
	case "enter":
		if r != nil {
			id := r.id()
			m.closeTree()
			m.page = pageTasks
			m.tselID = id
			m.rebuildTasks()
		}
	}
	return m, nil
}
