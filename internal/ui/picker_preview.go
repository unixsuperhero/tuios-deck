package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/unixsuperhero/tuios-deck/internal/panes"
)

// previewScreen is how many lines of a pane's screen a preview shows.
const previewScreen = 15

// shot is a pane's recent screen as a preview read it, kept per window id
// for as long as the picker is up.
type shot struct {
	lines []string
	err   string
	busy  bool
}

type previewMsg struct {
	id    string
	lines []string
	err   error
}

// previewPane is the pane whose screen a node's preview shows: the first
// live pane assigned to it.
func (m *Model) previewPane(n *pnode) *panes.Pane {
	if n == nil || m.protoRun.purpose != purposeAssign {
		return nil
	}
	var ps []*panes.Pane
	switch {
	case n.Task != nil:
		ps = m.assignedPanes("", n.Task.ID)
	case n.Project != nil:
		ps = m.assignedPanes(n.Project.ID, "")
	}
	if len(ps) == 0 {
		return nil
	}
	return ps[0]
}

// previewFetch reads the screen of the node's pane once per picker.
func (m *Model) previewFetch(n *pnode) tea.Cmd {
	p := m.previewPane(n)
	if p == nil || m.protoShots == nil || m.protoShots[p.ID] != nil {
		return nil
	}
	m.protoShots[p.ID] = &shot{busy: true}
	session, id := p.Session, p.ID
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
		defer cancel()
		c, err := m.client.CapturePane(ctx, session, id, 60, false)
		if err != nil {
			return previewMsg{id: id, err: err}
		}
		lines := strings.Split(strings.TrimRight(c.Content, "\n "), "\n")
		if len(lines) > previewScreen {
			lines = lines[len(lines)-previewScreen:]
		}
		return previewMsg{id: id, lines: lines}
	}
}

// takePreview files a screen a preview asked for.
func (m *Model) takePreview(msg previewMsg) {
	if m.protoShots == nil {
		return
	}
	s := &shot{lines: msg.lines}
	if msg.err != nil {
		s.err = msg.err.Error()
	}
	m.protoShots[msg.id] = s
}

// previewLines describes the highlighted node in width by height: a task's
// or a project's path, status, theme and assigned panes and the recent
// screen of the first of them; in the filter purpose, the items the filter
// would keep.
func (m *Model) previewLines(n *pnode, width, height int) []string {
	var lines []string
	field := func(label, value string) {
		lines = append(lines, cDim.Render(pad(label, 9))+value)
	}
	switch {
	case n == nil:
		lines = append(lines, cDim.Render("nothing highlighted"))
	case n.New && m.protoRun.purpose == purposeActive:
		lines = append(lines, cStatus.Render(newProject))
		lines = append(lines, "opens the new project form: its name, git repo and worktree;")
		lines = append(lines, "the project becomes the active one")
		if n.Text != "" {
			lines = append(lines, "  named "+n.Text)
		}
	case n.New:
		lines = append(lines, cStatus.Render(addNew))
		lines = append(lines, "opens the mass-add screen with")
		lines = append(lines, "  "+m.protoPrefillLine(n.Text))
	case m.protoRun.purpose == purposeFilter:
		f := nodeFilter(*n)
		lines = append(lines, nodeGlyph(*n)+" "+cTitle.Render(m.nodePath(*n)))
		var kept []string
		count := 0
		for _, c := range m.store.ContextItems() {
			if m.filterKeeps(f, c) {
				count++
				if len(kept) < height {
					kept = append(kept, kindStyle(c.Kind).Render(pad(c.Kind, 7))+" "+panes.OneLine(c.Text))
				}
			}
		}
		field("keeps", plural(count, "context item"))
		lines = append(lines, "")
		lines = append(lines, kept...)
	default:
		lines = append(lines, nodeGlyph(*n)+" "+cTitle.Render(nodeName(*n)))
		var ps []*panes.Pane
		if t := n.Task; t != nil {
			field("path", strings.ReplaceAll(m.taskPath(*t), " / ", " › "))
			field("status", t.Status)
			field("theme", t.Theme.Color+" "+t.Theme.Glyph)
			field("updated", ago(t.Updated))
			field("subtasks", fmt.Sprint(len(m.store.Children(t.ID))))
			ps = m.assignedPanes("", t.ID)
		} else if p := n.Project; p != nil {
			total, open := m.projectCounts(p.ID)
			field("path", p.Name)
			field("status", p.Status)
			field("theme", p.Theme.Color+" "+p.Theme.Glyph)
			field("tasks", fmt.Sprintf("%d (%d open)", total, open))
			ps = m.assignedPanes(p.ID, "")
		}
		field("panes", fmt.Sprint(len(ps)))
		for _, p := range ps {
			state := p.State
			if !p.IsAgent {
				state = p.Fg
			}
			lines = append(lines, "  "+p.Session+" › "+p.Name+"  "+stateStyle(p.State).Render(state))
		}
		if p := m.previewPane(n); p != nil {
			lines = append(lines, cBorder.Render(ansi.Truncate("── screen of "+p.Name+" "+strings.Repeat("─", width), width, "")))
			s := m.protoShots[p.ID]
			switch {
			case s == nil || s.busy:
				lines = append(lines, cDim.Render("reading…"))
			case s.err != "":
				lines = append(lines, cErr.Render(s.err))
			default:
				lines = append(lines, s.lines...)
			}
		}
	}
	for i := range lines {
		lines[i] = ansi.Truncate(lines[i], width, "…")
	}
	if len(lines) > height {
		// The screen's last lines matter most; keep the head and the end.
		head := 0
		for i, l := range lines {
			if strings.Contains(l, "── screen of") {
				head = i + 1
				break
			}
		}
		if head > 0 && head < height {
			lines = append(lines[:head:head], lines[len(lines)-(height-head):]...)
		} else {
			lines = lines[:height]
		}
	}
	for len(lines) < height {
		lines = append(lines, "")
	}
	return lines
}

// protoPrefillLine is the mass-add outline on one line.
func (m *Model) protoPrefillLine(typed string) string {
	return strings.ReplaceAll(m.protoPrefill(typed), "\n  ", " › ")
}

// ago is a unix time as a short age.
func ago(unix int64) string {
	if unix == 0 {
		return "never"
	}
	d := time.Since(time.Unix(unix, 0))
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	}
	return fmt.Sprintf("%dd ago", int(d.Hours()/24))
}
