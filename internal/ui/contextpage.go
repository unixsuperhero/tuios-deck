package ui

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	ctxitem "github.com/unixsuperhero/tuios-deck/internal/context"
	"github.com/unixsuperhero/tuios-deck/internal/panes"
	"github.com/unixsuperhero/tuios-deck/internal/store"
)

// ctxPage is the state of the context page: the rows drawn, the selected
// item by id so it survives a refresh, and how the list is sorted,
// grouped and filtered.
type ctxPage struct {
	rows   []crow
	sel    int
	selID  string
	sort   ctxSort
	group  ctxGroup
	filter ctxFilter
	// scopeCurrent limits the list to the items of scopePane; the pane is
	// the one selected on the deck when the page was opened, and is kept
	// while another is not selected.
	scopeCurrent bool
	scopePane    ctxScopePane
}

// ctxScopePane names the pane the "current" scope keeps.
type ctxScopePane struct {
	Session, Window, Name string
}

// crow is a line of the context page: a group header or an item.
type crow struct {
	Header string
	Item   *store.ContextItem
}

type ctxSort int

const (
	sortCreated ctxSort = iota
	sortKind
	sortStarred
	sortPinned
	ctxSorts
)

func (s ctxSort) String() string {
	return [...]string{"newest", "kind", "starred first", "pinned first"}[s]
}

type ctxGroup int

const (
	ctxNone ctxGroup = iota
	ctxByKind
	ctxByProject
	ctxByTask
	ctxByPane
	ctxGroups
)

func (g ctxGroup) String() string {
	return [...]string{"none", "kind", "project", "task", "pane"}[g]
}

// ctxFilter narrows the list. Empty fields do not narrow.
type ctxFilter struct {
	Kind      string
	ProjectID string
	TaskID    string
	Text      string
}

func (f ctxFilter) empty() bool { return f == ctxFilter{} }

var (
	cStar  = lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
	cPin   = lipgloss.NewStyle().Foreground(lipgloss.Color("6"))
	kindFg = map[string]string{
		"link": "4", "pr": "5", "file": "2", "dir": "2", "prompt": "3", "command": "6",
	}
)

func kindStyle(k string) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(lipgloss.Color(kindFg[k]))
}

// openContext shows the context page.
func (m *Model) openContext() {
	m.page = pageContext
	if p := m.selected(); p != nil {
		m.cx.scopePane = ctxScopePane{p.Session, p.ID, p.Name}
	}
	m.rebuildContext()
}

// toggleCtxScope flips between the current pane's items and all items.
func (m *Model) toggleCtxScope() {
	m.cx.scopeCurrent = !m.cx.scopeCurrent
	m.rebuildContext()
	if m.cx.scopeCurrent {
		if m.cx.scopePane.Window == "" {
			m.setStatus("scope current: no pane selected on the deck (esc, pick one, I)")
		} else {
			m.setStatus("scope current: " + m.cx.scopePane.Name)
		}
	} else {
		m.setStatus("scope all panes")
	}
}

func (m *Model) ctxMatches(c store.ContextItem) bool {
	return m.inScope(c) && m.filterKeeps(m.cx.filter, c)
}

// inScope says whether the page's scope, all panes or the current one,
// holds the item.
func (m *Model) inScope(c store.ContextItem) bool {
	return !m.cx.scopeCurrent || c.Window == m.cx.scopePane.Window && c.Session == m.cx.scopePane.Session
}

// scopedItems is every item the page's scope holds, before the filter.
func (m *Model) scopedItems() []store.ContextItem {
	var out []store.ContextItem
	for _, c := range m.store.ContextItems() {
		if m.inScope(c) {
			out = append(out, c)
		}
	}
	return out
}

// filterKeeps says whether the filter lets the item through. A task keeps
// the items of its subtasks too.
func (m *Model) filterKeeps(f ctxFilter, c store.ContextItem) bool {
	switch {
	case f.Kind != "" && c.Kind != f.Kind,
		f.ProjectID != "" && c.ProjectID != f.ProjectID,
		f.TaskID != "" && (c.TaskID == "" || !m.subtree(f.TaskID)[c.TaskID]),
		f.Text != "" && !strings.Contains(strings.ToLower(c.Text), strings.ToLower(f.Text)):
		return false
	}
	return true
}

func kindRank(k string) int {
	for i, c := range store.ContextKinds() {
		if c == k {
			return i
		}
	}
	return len(store.ContextKinds())
}

// ctxGroupKey is the header an item sits under in the current grouping.
func (m *Model) ctxGroupKey(c store.ContextItem) string {
	switch m.cx.group {
	case ctxByKind:
		return c.Kind
	case ctxByProject:
		if p := m.store.Project(c.ProjectID); p != nil {
			return p.Name
		}
	case ctxByTask:
		if t := m.store.Task(c.TaskID); t != nil {
			return m.taskPath(*t)
		}
	case ctxByPane:
		return c.Session + " › " + c.Pane
	}
	return "(none)"
}

// rebuildContext re-reads the store into the rows of the context page:
// filtered, sorted, then grouped with the groups in the order their first
// item comes. The selection follows the item's id, and stays at the same
// place in the list when that item is gone.
func (m *Model) rebuildContext() {
	var items []store.ContextItem
	for _, c := range m.store.ContextItems() {
		if m.ctxMatches(c) {
			items = append(items, c)
		}
	}
	sort.SliceStable(items, func(i, j int) bool {
		a, b := items[i], items[j]
		switch m.cx.sort {
		case sortKind:
			return kindRank(a.Kind) < kindRank(b.Kind)
		case sortStarred:
			return a.Starred && !b.Starred
		case sortPinned:
			return a.Pinned && !b.Pinned
		}
		return false // the store's order is newest first
	})
	var rows []crow
	if m.cx.group == ctxNone {
		for i := range items {
			rows = append(rows, crow{Item: &items[i]})
		}
	} else {
		var order []string
		byKey := map[string][]int{}
		for i, c := range items {
			k := m.ctxGroupKey(c)
			if _, ok := byKey[k]; !ok {
				order = append(order, k)
			}
			byKey[k] = append(byKey[k], i)
		}
		for _, k := range order {
			rows = append(rows, crow{Header: k})
			for _, i := range byKey[k] {
				rows = append(rows, crow{Item: &items[i]})
			}
		}
	}
	m.cx.rows = rows
	found := -1
	for i, r := range rows {
		if r.Item != nil && r.Item.ID == m.cx.selID {
			found = i
			break
		}
	}
	if found < 0 {
		found = m.cx.sel
		if found >= len(rows) {
			found = len(rows) - 1
		}
		if found >= 0 && rows[found].Item == nil {
			if next := m.ctxNext(found, 1); next >= 0 {
				found = next
			} else {
				found = m.ctxNext(found, -1)
			}
		}
	}
	m.ctxSelect(found)
}

// ctxNext is the first item row from start in direction dir, or -1.
func (m *Model) ctxNext(start, dir int) int {
	for i := start; i >= 0 && i < len(m.cx.rows); i += dir {
		if m.cx.rows[i].Item != nil {
			return i
		}
	}
	return -1
}

func (m *Model) ctxSelect(i int) {
	if i < 0 || i >= len(m.cx.rows) || m.cx.rows[i].Item == nil {
		m.cx.sel, m.cx.selID = -1, ""
		if i >= 0 && i < len(m.cx.rows) {
			m.cx.sel = i
		}
		return
	}
	m.cx.sel, m.cx.selID = i, m.cx.rows[i].Item.ID
}

func (m *Model) ctxMove(dir int) {
	if next := m.ctxNext(m.cx.sel+dir, dir); next >= 0 {
		m.ctxSelect(next)
	}
}

// selectedItem is the context page's selected item, or nil.
func (m *Model) selectedItem() *store.ContextItem {
	if m.cx.sel >= 0 && m.cx.sel < len(m.cx.rows) {
		return m.cx.rows[m.cx.sel].Item
	}
	return nil
}

func (m *Model) handleContextKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	it := m.selectedItem()
	switch msg.String() {
	case "esc":
		m.page = pageDeck
		return m, m.loadDetail(true)
	case "j", "down":
		m.ctxMove(1)
	case "k", "up":
		m.ctxMove(-1)
	case "{", "}":
		m.ctxJump(map[string]int{"{": -1, "}": 1}[msg.String()])
	case "g", "home":
		m.ctxSelect(m.ctxNext(0, 1))
	case "G", "end":
		m.ctxSelect(m.ctxNext(len(m.cx.rows)-1, -1))
	case "r":
		m.rebuildContext()
		m.setStatus(plural(len(m.store.ContextItems()), "context item"))
	case "s":
		m.cx.sort = (m.cx.sort + 1) % ctxSorts
		m.rebuildContext()
		m.setStatus("sorted " + m.cx.sort.String())
	case "o", "O":
		step := ctxGroup(1)
		if msg.String() == "O" {
			step = ctxGroups - 1
		}
		m.cx.group = (m.cx.group + step) % ctxGroups
		m.rebuildContext()
		m.setStatus("grouped by " + m.cx.group.String())
	case "f":
		return m, m.openContextFilter()
	case "t":
		m.toggleCtxScope()
	case "*":
		if it == nil {
			return m, nil
		}
		word := "starred"
		if it.Starred {
			word = "unstarred"
		}
		return m, m.ctxChange(m.store.SetContextStar(it.ID, !it.Starred), word+" "+shortText(it.Text))
	case "!", "P":
		if it == nil {
			return m, nil
		}
		word := "pinned"
		if it.Pinned {
			word = "unpinned"
		}
		return m, m.ctxChange(m.store.SetContextPin(it.ID, !it.Pinned), word+" "+shortText(it.Text))
	case "a":
		return m, m.openContextAssign()
	case "y":
		if it == nil {
			return m, nil
		}
		return m, m.copyItem(*it)
	case "enter":
		if it == nil {
			return m, nil
		}
		return m, m.insertItem(*it)
	case "ctrl+o":
		if it == nil {
			return m, nil
		}
		return m, m.openItem(*it)
	case "w":
		if it == nil {
			return m, nil
		}
		return m, m.openWithForm(*it)
	case "x":
		if it == nil {
			return m, nil
		}
		id, text := it.ID, shortText(it.Text)
		m.mode = modeConfirm
		m.confirm.Text = "delete " + it.Kind + " " + text + "? (Y/n)"
		m.confirm.Do = func() tea.Msg {
			if err := m.store.DeleteContextItem(id); err != nil {
				return actionMsg{err: err}
			}
			return actionMsg{status: "deleted " + text}
		}
	}
	return m, nil
}

func shortText(s string) string { return ansi.Truncate(panes.OneLine(s), 40, "…") }

// ctxChange reports a store call made on the spot and redraws the list.
func (m *Model) ctxChange(err error, status string) tea.Cmd {
	if err != nil {
		m.setStatus("error: " + err.Error())
	} else {
		m.setStatus(status)
	}
	m.rebuildContext()
	return nil
}

// openContextFilter puts up the tree of kinds, projects and tasks that
// hold context items; picking one filters the page.
func (m *Model) openContextFilter() tea.Cmd {
	f := m.cx.filter
	return m.openTreeView(protoRun{purpose: purposeFilter, title: "filter context"}, f.ProjectID, f.TaskID)
}

// ctxFilterText is the filter in words, for the title and the status.
func (m *Model) ctxFilterText() string {
	f := m.cx.filter
	var parts []string
	if f.Kind != "" {
		parts = append(parts, "kind "+f.Kind)
	}
	if p := m.store.Project(f.ProjectID); p != nil {
		parts = append(parts, "project "+p.Name)
	}
	if t := m.store.Task(f.TaskID); t != nil {
		parts = append(parts, "task "+t.Title)
	}
	if f.Text != "" {
		parts = append(parts, "text "+f.Text)
	}
	return strings.Join(parts, ", ")
}

// openContextAssign ties the selected item to a task or a project through
// the assign tree.
func (m *Model) openContextAssign() tea.Cmd {
	it := m.selectedItem()
	if it == nil {
		m.setStatus("select a context item first")
		return nil
	}
	text := shortText(it.Text)
	run := protoRun{purpose: purposeAssign, title: "assign " + text, itemID: it.ID, name: text}
	return m.openTreeView(run, it.ProjectID, it.TaskID)
}

// assignItem ties a context item to the task or the project.
func (m *Model) assignItem(id, text, taskID, projectID string) tea.Cmd {
	return func() tea.Msg {
		if err := m.store.AssignContext(id, taskID, projectID); err != nil {
			return actionMsg{err: err}
		}
		return actionMsg{status: "assigned " + text + " to " + m.ctxPath(*m.store.ContextItem(id))}
	}
}

// ctxPath is "project › top task › task" for an item, or "".
func (m *Model) ctxPath(c store.ContextItem) string {
	path := ""
	if p := m.store.Project(c.ProjectID); p != nil {
		path = p.Name
	}
	if c.TaskID != "" {
		for _, t := range m.store.Path(c.TaskID) {
			path += " › " + t.Title
		}
	}
	return path
}

// extractContext reads the selected pane's scrollback, activity,
// transcript and command history and stores the items that are new.
func (m *Model) extractContext() tea.Cmd {
	p := m.selected()
	if p == nil {
		m.setStatus("select a pane first")
		return nil
	}
	src := ctxitem.Source{Session: p.Session, Window: p.ID, Pane: p.Name, Cwd: p.Cwd}
	nonce := m.nonce
	m.setStatus("extracting context from " + p.Name)
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 3*callTimeout)
		defer cancel()
		items, _, err := ctxitem.Gather(ctx, m.client, src, nonce)
		if err != nil {
			return actionMsg{err: err}
		}
		added, err := m.store.AddContextItems(items)
		if err != nil {
			return actionMsg{err: err}
		}
		return actionMsg{status: fmt.Sprintf("%s from %s (%d found)", plural(added, "new context item"), src.Pane, len(items))}
	}
}

// ctxTitle is what the title line says about the context page's view.
func (m *Model) ctxTitle() string {
	s := "scope all panes"
	if m.cx.scopeCurrent {
		s = "scope current: " + m.cx.scopePane.Name
		if m.cx.scopePane.Window == "" {
			s = "scope current: no pane"
		}
	}
	s += " · sort " + m.cx.sort.String()
	if m.cx.group != ctxNone {
		s += " · by " + m.cx.group.String()
	}
	if !m.cx.filter.empty() {
		s += " · " + m.ctxFilterText()
	}
	return s
}

// ctxMarks are the star and the pin of an item, two cells wide.
func ctxMarks(c store.ContextItem) string {
	star, pin := " ", " "
	if c.Starred {
		star = cStar.Render("★")
	}
	if c.Pinned {
		pin = cPin.Render("^")
	}
	return star + pin
}

// contextListLines draws the items of the context page: the star and the
// pin, the kind, and the text on one line.
func (m *Model) contextListLines(width, height int) []string {
	lines := make([]string, 0, height)
	if len(m.cx.rows) == 0 {
		if m.cx.scopeCurrent && m.cx.scopePane.Window == "" {
			lines = append(lines, cDim.Render("scope is the current pane, but none was selected"),
				cDim.Render("esc, select a pane on the deck, then I; or t for all"))
		} else if m.cx.scopeCurrent && len(m.store.ContextItems()) > 0 {
			lines = append(lines, cDim.Render("no items from "+m.cx.scopePane.Name+" (t shows all, i extracts)"))
		} else if len(m.store.ContextItems()) == 0 {
			lines = append(lines, cDim.Render("no context items: press i on a pane"))
		} else {
			lines = append(lines, cDim.Render("nothing matches the filter (f)"))
		}
	}
	start := 0
	if m.cx.sel >= height {
		start = m.cx.sel - height + 1
	}
	for i := start; i < len(m.cx.rows) && len(lines) < height; i++ {
		r := m.cx.rows[i]
		if r.Item == nil {
			lines = append(lines, cHeader.Render(ansi.Truncate("▾ "+r.Header, width, "…")))
			continue
		}
		c := *r.Item
		kind := kindStyle(c.Kind).Render(pad(c.Kind, 7))
		text := ansi.Truncate(panes.OneLine(c.Text), width-11, "…")
		line := " " + ctxMarks(c) + " " + kind + " " + text
		if i == m.cx.sel {
			line = cSel.Render(ansi.Strip(pad(line, width)))
		}
		lines = append(lines, line)
	}
	for len(lines) < height {
		lines = append(lines, "")
	}
	return lines
}

// contextDetailLines shows the selected item on the right: its whole text,
// wrapped, then where it came from and what it is assigned to.
func (m *Model) contextDetailLines(width, height int) []string {
	var lines []string
	field := func(label, value string) {
		lines = append(lines, cDim.Render(pad(label, 9))+value)
	}
	c := m.selectedItem()
	if c == nil {
		lines = append(lines, cDim.Render(" no context item selected"))
	} else {
		lines = append(lines, kindStyle(c.Kind).Render(c.Kind)+" "+ctxMarks(*c))
		lines = append(lines, "")
		for _, l := range strings.Split(ansi.Wrap(c.Text, width, ""), "\n") {
			lines = append(lines, l)
		}
		lines = append(lines, "")
		field("kind", c.Kind)
		field("pane", c.Session+" › "+c.Pane)
		path := m.ctxPath(*c)
		if path == "" {
			path = cDim.Render("(none: a assigns)")
		}
		field("assigned", path)
		field("created", time.Unix(c.Created, 0).Format("2006-01-02 15:04"))
		field("starred", yesNo(c.Starred))
		field("pinned", yesNo(c.Pinned))
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

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}
