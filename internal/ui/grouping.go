package ui

import (
	"os"
	"sort"
	"strings"

	"github.com/unixsuperhero/tuios-deck/internal/panes"
	"github.com/unixsuperhero/tuios-deck/internal/store"
)

// grouping is how the left rail of the deck page is grouped: a header row
// per group, with the group's panes under it.
type grouping int

const (
	bySession grouping = iota
	byProject
	byTask
	byTopTask
	byCwd
	byHarness
	byState
	byKind
	groupings // the count, for cycling
)

func (g grouping) String() string {
	return [...]string{"session", "project", "task", "top-level task", "cwd", "harness", "state", "kind"}[g]
}

// parseGrouping is the grouping called name, as String spells it.
func parseGrouping(name string) (grouping, bool) {
	for g := bySession; g < groupings; g++ {
		if g.String() == name {
			return g, true
		}
	}
	return 0, false
}

const noGroup = "(none)"

// groupKey is the group a pane falls in under the grouping: a key that
// tells groups apart, the label its header shows, and the theme the
// header is drawn in when the group is a project or a task.
func (m *Model) groupKey(p *panes.Pane) (key, label string, theme *store.Theme) {
	switch m.grouping {
	case byProject:
		project, _, _ := m.store.Resolve(p.ID)
		if project == nil {
			return noGroup, noGroup, nil
		}
		return project.ID, project.Name, &project.Theme
	case byTask:
		project, task, _ := m.store.Resolve(p.ID)
		if task == nil {
			return noGroup, noGroup, nil
		}
		label := "?"
		if project != nil {
			label = project.Name
		}
		for _, c := range m.store.Path(task.ID) {
			label += " › " + c.Title
		}
		return task.ID, label, &task.Theme
	case byTopTask:
		project, _, top := m.store.Resolve(p.ID)
		if top == nil {
			return noGroup, noGroup, nil
		}
		label := "?"
		if project != nil {
			label = project.Name
		}
		return top.ID, label + " › " + top.Title, &top.Theme
	case byCwd:
		if p.Cwd == "" {
			return noGroup, noGroup, nil
		}
		return p.Cwd, shortenHome(p.Cwd), nil
	case byHarness:
		if p.Harness == "" {
			return noGroup, noGroup, nil
		}
		return p.Harness, p.Harness, nil
	case byState:
		if !p.IsAgent {
			return "terminal", "terminal", nil
		}
		if p.State == "" {
			return noGroup, noGroup, nil
		}
		return p.State, p.State, nil
	case byKind:
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
// under the current grouping. By session the rows follow the daemon's
// order with the session on each header; under any other grouping the
// groups sort by label with "(none)" last, and the panes inside a group
// keep their session and window order.
func (m *Model) groupedRows() []row {
	var rows []row
	if m.grouping == bySession {
		for i := range m.merged {
			s := &m.merged[i]
			rows = append(rows, row{Header: s.Name, Session: s})
			for j := range s.Panes {
				rows = append(rows, row{Pane: &s.Panes[j]})
			}
		}
		return rows
	}
	type group struct {
		label string
		theme *store.Theme
		panes []*panes.Pane
	}
	var order []string
	groups := map[string]*group{}
	for i := range m.merged {
		s := &m.merged[i]
		for j := range s.Panes {
			p := &s.Panes[j]
			key, label, theme := m.groupKey(p)
			g, ok := groups[key]
			if !ok {
				g = &group{label: label, theme: theme}
				groups[key] = g
				order = append(order, key)
			}
			g.panes = append(g.panes, p)
		}
	}
	sort.SliceStable(order, func(i, j int) bool {
		a, b := groups[order[i]].label, groups[order[j]].label
		if (a == noGroup) != (b == noGroup) {
			return b == noGroup
		}
		return strings.ToLower(a) < strings.ToLower(b)
	})
	for _, key := range order {
		g := groups[key]
		rows = append(rows, row{Header: g.label, Theme: g.theme})
		for _, p := range g.panes {
			rows = append(rows, row{Pane: p})
		}
	}
	return rows
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
	m.rebuildRows()
	m.setStatus("grouped by " + g.String())
}

// cycleGrouping moves to the next grouping, or the previous one with a
// negative step.
func (m *Model) cycleGrouping(step int) {
	n := int(groupings)
	m.setGrouping(grouping(((int(m.grouping)+step)%n + n) % n))
}
