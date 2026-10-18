package ui

import (
	"os"
	"sort"
	"strings"

	"github.com/unixsuperhero/tuios-deck/internal/panes"
	"github.com/unixsuperhero/tuios-deck/internal/store"
)

// level is one way of sorting panes into groups: by session, project, and
// so on. A grouping nests levels, the first outermost.
type level int

const (
	lvSession level = iota
	lvProject
	lvTask
	lvParentTask
	lvTopTask
	lvCwd
	lvHarness
	lvState
	lvKind
	levels // the count
)

func (l level) String() string {
	return [...]string{"session", "project", "task", "parent task", "top-level task", "cwd", "harness", "state", "kind"}[l]
}

// levelNames is every level's name, for completion.
func levelNames() []string {
	var out []string
	for l := lvSession; l < levels; l++ {
		out = append(out, l.String())
	}
	return out
}

// parseLevel is the level called name, ignoring case and spaces around it.
// "top" and "parent" are short for top-level task and parent task.
func parseLevel(name string) (level, bool) {
	name = strings.ToLower(strings.Join(strings.Fields(name), " "))
	switch name {
	case "top", "top task":
		return lvTopTask, true
	case "parent":
		return lvParentTask, true
	}
	for l := lvSession; l < levels; l++ {
		if l.String() == name {
			return l, true
		}
	}
	return 0, false
}

// grouping is how the left rail of the deck page is grouped: a list of
// levels, each a header row per group with the next level nested under it
// and the panes under the deepest one. An empty grouping is by session.
type grouping []level

func (g grouping) String() string {
	if len(g) == 0 {
		return lvSession.String()
	}
	names := make([]string, len(g))
	for i, l := range g {
		names[i] = l.String()
	}
	return strings.Join(names, " > ")
}

// bySession is whether the grouping is the plain session one, which the
// title line leaves unsaid.
func (g grouping) bySession() bool {
	return len(g) == 0 || len(g) == 1 && g[0] == lvSession
}

// presets are the groupings o and O cycle through, in order.
var presets = []grouping{
	{lvSession},
	{lvProject},
	{lvProject, lvParentTask},
	{lvTask},
	{lvTopTask},
	{lvHarness, lvProject, lvParentTask},
	{lvCwd},
	{lvHarness},
	{lvState},
	{lvKind},
}

// parseGrouping reads a grouping written as level names joined by ">",
// such as "project > parent task". It refuses an empty text or an unknown
// level.
func parseGrouping(text string) (grouping, bool) {
	var g grouping
	for _, part := range strings.Split(text, ">") {
		l, ok := parseLevel(part)
		if !ok {
			return nil, false
		}
		g = append(g, l)
	}
	return g, true
}

const noGroup = "(none)"

// groupKey is the group a pane falls in at one level of the grouping: a key
// that tells groups apart, the label its header shows, and the theme the
// header is drawn in when the group is a project or a task. underProject
// says a project level sits above, so a task label leaves the project out.
func (m *Model) groupKey(l level, p *panes.Pane, underProject bool) (key, label string, theme *store.Theme) {
	taskLabel := func(project *store.Project, t *store.Task) (string, string, *store.Theme) {
		var titles []string
		for _, c := range m.store.Path(t.ID) {
			titles = append(titles, c.Title)
		}
		if !underProject {
			name := "?"
			if project != nil {
				name = project.Name
			}
			titles = append([]string{name}, titles...)
		}
		return t.ID, strings.Join(titles, " › "), &t.Theme
	}
	switch l {
	case lvProject:
		project, _, _ := m.store.Resolve(p.ID)
		if project == nil {
			return noGroup, noGroup, nil
		}
		return project.ID, project.Name, &project.Theme
	case lvTask, lvParentTask, lvTopTask:
		project, task, top := m.store.Resolve(p.ID)
		switch {
		case task == nil:
			return noGroup, noGroup, nil
		case l == lvTopTask:
			task = top
		case l == lvParentTask && task.ParentID != "":
			if parent := m.store.Task(task.ParentID); parent != nil {
				task = parent
			}
		}
		return taskLabel(project, task)
	case lvCwd:
		if p.Cwd == "" {
			return noGroup, noGroup, nil
		}
		return p.Cwd, shortenHome(p.Cwd), nil
	case lvHarness:
		if p.Harness == "" {
			return noGroup, noGroup, nil
		}
		return p.Harness, p.Harness, nil
	case lvState:
		if !p.IsAgent {
			return "terminal", "terminal", nil
		}
		if p.State == "" {
			return noGroup, noGroup, nil
		}
		return p.State, p.State, nil
	case lvKind:
		if p.IsAgent {
			return "agents", "agents", nil
		}
		return "terminals", "terminals", nil
	}
	return p.Session, p.Session, nil
}

// shortenHome writes the person's home folder as ~.
func shortenHome(path string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" || home == "/" {
		return path
	}
	if path == home {
		return "~"
	}
	if strings.HasPrefix(path, home+"/") {
		return "~" + path[len(home):]
	}
	return path
}

// groupedRows builds the rows of the left rail from the merged listing
// under the current grouping. By session alone the rows follow the
// daemon's order with the session on each header, empty sessions too.
// Otherwise each level nests under the one before it: groups sort by label
// with "(none)" last (sessions keep the daemon's order), and the panes
// inside a group keep their session and window order.
func (m *Model) groupedRows() []row {
	var rows []row
	if m.grouping.bySession() {
		for i := range m.merged {
			s := &m.merged[i]
			rows = append(rows, row{Header: s.Name, Session: s})
			for j := range s.Panes {
				rows = append(rows, row{Pane: &s.Panes[j], Depth: 1})
			}
		}
		return rows
	}
	var all []*panes.Pane
	for i := range m.merged {
		for j := range m.merged[i].Panes {
			all = append(all, &m.merged[i].Panes[j])
		}
	}
	m.nestRows(&rows, all, 0, false)
	return rows
}

// nestRows appends the rows for ps grouped from the grouping's level at
// depth down.
func (m *Model) nestRows(rows *[]row, ps []*panes.Pane, depth int, underProject bool) {
	if depth == len(m.grouping) {
		for _, p := range ps {
			*rows = append(*rows, row{Pane: p, Depth: depth})
		}
		return
	}
	l := m.grouping[depth]
	type group struct {
		label   string
		theme   *store.Theme
		session *panes.Session
		panes   []*panes.Pane
	}
	var order []string
	groups := map[string]*group{}
	for _, p := range ps {
		key, label, theme := m.groupKey(l, p, underProject)
		g, ok := groups[key]
		if !ok {
			g = &group{label: label, theme: theme}
			if l == lvSession {
				g.session = m.sessionNamed(p.Session)
			}
			groups[key] = g
			order = append(order, key)
		}
		g.panes = append(g.panes, p)
	}
	if l != lvSession {
		sort.SliceStable(order, func(i, j int) bool {
			a, b := groups[order[i]].label, groups[order[j]].label
			if (a == noGroup) != (b == noGroup) {
				return b == noGroup
			}
			return strings.ToLower(a) < strings.ToLower(b)
		})
	}
	for _, key := range order {
		g := groups[key]
		*rows = append(*rows, row{Header: g.label, Theme: g.theme, Session: g.session, Depth: depth})
		m.nestRows(rows, g.panes, depth+1, underProject || l == lvProject)
	}
}

// sessionNamed is the merged session with that name, or nil.
func (m *Model) sessionNamed(name string) *panes.Session {
	for i := range m.merged {
		if m.merged[i].Name == name {
			return &m.merged[i]
		}
	}
	return nil
}

// rebuildRows redraws the left rail from the last listing under the
// current grouping, keeping the selection on the same pane.
func (m *Model) rebuildRows() {
	selected := m.selectedID()
	m.rows = m.groupedRows()
	newSel := -1
	for i, r := range m.rows {
		if r.Pane != nil && r.Pane.ID == selected {
			newSel = i
			break
		}
	}
	if newSel >= 0 {
		m.sel = newSel
	} else {
		m.sel = m.firstPane(0, 1)
	}
}

// setGrouping regroups the left rail without a daemon call and says so.
func (m *Model) setGrouping(g grouping) {
	m.grouping = g
	m.railUp = 0
	m.rebuildRows()
	m.setStatus("grouped by " + g.String())
}

// cycleGrouping moves to the next preset grouping, or the previous one with
// a negative step. From a grouping that is no preset it starts at the
// first (or the last going back).
func (m *Model) cycleGrouping(step int) {
	n := len(presets)
	cur := -1
	for i, g := range presets {
		if g.String() == m.grouping.String() {
			cur = i
			break
		}
	}
	if cur < 0 && step < 0 {
		cur = 0
	}
	m.setGrouping(presets[((cur+step)%n+n)%n])
}
