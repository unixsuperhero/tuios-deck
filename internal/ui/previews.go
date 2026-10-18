package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/unixsuperhero/tuios-deck/internal/panes"
	"github.com/unixsuperhero/tuios-deck/internal/store"
)

// previewLines is how many lines of a pane's recent output a preview shows,
// and previewRefresh how long a shown capture is left before it is read
// again while the same entry stays highlighted.
const (
	previewLines   = 6
	previewRefresh = 2 * time.Second
)

// previewSpec is what a field's Preview returns for one option: the lines
// worked out at once, and the pane whose recent output follows them. An
// empty Head draws nothing; an empty Key draws the head alone.
type previewSpec struct {
	Head            []string
	Key             string // names the capture; the session and the window id joined
	Session, Window string
}

// previewState is the capture the form box is showing. Seq discards a
// capture that comes back after a newer entry was highlighted.
type previewState struct {
	key   string
	seq   int
	busy  bool
	at    time.Time
	lines []string
	err   string
}

type formPreviewMsg struct {
	seq   int
	key   string
	lines []string
	err   error
}

// highlighted is the option the person is on: the highlighted list entry,
// or a typed value that is exactly an option.
func (f *form) highlighted() string {
	cur := f.Fields[f.Cur]
	if cur.Preview == nil || cur.at > 0 {
		return ""
	}
	if list := f.list(); cur.hl > 0 && cur.hl <= len(list) && list[cur.hl-1] != addNew {
		return list[cur.hl-1]
	}
	if cur.isOption() {
		return strings.TrimSpace(cur.Value)
	}
	return ""
}

func (m *Model) currentPreview() (previewSpec, bool) {
	if m.mode != modeForm || m.form == nil {
		return previewSpec{}, false
	}
	opt := m.form.highlighted()
	if opt == "" {
		return previewSpec{}, false
	}
	spec := m.form.Fields[m.form.Cur].Preview(opt)
	return spec, len(spec.Head) > 0
}

// previewCmd starts the capture of the highlighted entry's pane when it
// differs from the one shown, or, with tick set, when the shown one is old.
// It never touches what is drawn until the capture lands, and a capture
// for an entry no longer highlighted is dropped on arrival.
func (m *Model) previewCmd(tick bool) tea.Cmd {
	spec, ok := m.currentPreview()
	if !ok || spec.Key == "" {
		m.previews.key = ""
		m.previews.seq++
		m.previews.busy = false
		return nil
	}
	pv := &m.previews
	if pv.key == spec.Key {
		if !tick || pv.busy || time.Since(pv.at) < previewRefresh {
			return nil
		}
	} else {
		*pv = previewState{key: spec.Key, seq: pv.seq + 1}
	}
	pv.busy = true
	seq, key, session, window := pv.seq, spec.Key, spec.Session, spec.Window
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
		defer cancel()
		c, err := m.client.CapturePane(ctx, session, window, previewLines*4, false)
		if err != nil {
			return formPreviewMsg{seq: seq, key: key, err: err}
		}
		return formPreviewMsg{seq: seq, key: key, lines: tailLines(c.Content, previewLines)}
	}
}

// applyPreview keeps a capture only if it is for the entry still shown.
func (m *Model) applyPreview(msg formPreviewMsg) {
	pv := &m.previews
	if msg.seq != pv.seq || msg.key != pv.key {
		return
	}
	pv.busy, pv.at = false, time.Now()
	if msg.err != nil {
		pv.err = msg.err.Error()
		return
	}
	pv.err, pv.lines = "", msg.lines
}

// tailLines is the last n non-blank-ended lines of text.
func tailLines(text string, n int) []string {
	lines := strings.Split(strings.TrimRight(text, " \t\r\n"), "\n")
	if len(lines) == 1 && lines[0] == "" {
		return nil
	}
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	for i, l := range lines {
		lines[i] = strings.TrimRight(ansi.Strip(l), " \t\r")
	}
	return lines
}

// previewBox is the lines drawn under the current field's list, cut to
// room lines; with too little room for a title and a line, nothing.
func (m *Model) previewBox(iw, room int) []string {
	if room < 2 {
		return nil
	}
	out := m.previewAll(iw)
	if len(out) > room {
		out = out[:room]
	}
	return out
}

func (m *Model) previewAll(iw int) []string {
	spec, ok := m.currentPreview()
	if !ok {
		return nil
	}
	out := []string{cDim.Render("    ┄ preview")}
	for _, l := range spec.Head {
		out = append(out, ansi.Truncate("    "+panes.OneLine(l), iw, "…"))
	}
	if spec.Key == "" {
		return out
	}
	pv := m.previews
	switch {
	case pv.key != spec.Key || (pv.busy && pv.lines == nil && pv.err == ""):
		out = append(out, cDim.Render("    loading recent output…"))
	case pv.err != "":
		out = append(out, cDim.Render(ansi.Truncate("    capture failed: "+panes.OneLine(pv.err), iw, "…")))
	case len(pv.lines) == 0:
		out = append(out, cDim.Render("    (no output yet)"))
	default:
		for _, l := range pv.lines {
			out = append(out, cDim.Render(ansi.Truncate("    │ "+l, iw, "…")))
		}
	}
	return out
}

// paneByID finds a live pane in the last listing.
func (m *Model) paneByID(id string) *panes.Pane {
	for _, s := range m.merged {
		for i := range s.Panes {
			if s.Panes[i].ID == id {
				return &s.Panes[i]
			}
		}
	}
	return nil
}

func paneLine(p *panes.Pane) string {
	kind := "terminal"
	if p.IsAgent {
		kind = "agent"
		if p.Harness != "" {
			kind = p.Harness
		}
	}
	s := strings.Join([]string{p.Name, p.Session, kind}, " · ")
	if p.State != "" {
		s += " · " + p.State
	}
	if p.NeedsYou {
		s += " · needs you"
	}
	return s
}

// relevantPanes are the live panes assigned to the task, or to anything
// under it, or to the project when task is nil; panes assigned to the task
// itself come first, then needs-you ones, then agents.
func (m *Model) relevantPanes(task *store.Task, project *store.Project) []*panes.Pane {
	var direct, rest []*panes.Pane
	for w := range m.store.Assignments() {
		p := m.paneByID(w)
		if p == nil {
			continue
		}
		pr, t, _ := m.store.Resolve(w)
		switch {
		case task != nil && t != nil && t.ID == task.ID:
			direct = append(direct, p)
		case task != nil && t != nil && pathHas(m.store.Path(t.ID), task.ID):
			rest = append(rest, p)
		case project != nil && pr != nil && pr.ID == project.ID:
			rest = append(rest, p)
		}
	}
	rank := func(ps []*panes.Pane) {
		for i := 1; i < len(ps); i++ {
			for j := i; j > 0 && paneRank(ps[j]) < paneRank(ps[j-1]); j-- {
				ps[j], ps[j-1] = ps[j-1], ps[j]
			}
		}
	}
	rank(direct)
	rank(rest)
	return append(direct, rest...)
}

func pathHas(path []store.Task, id string) bool {
	for _, t := range path {
		if t.ID == id {
			return true
		}
	}
	return false
}

func paneRank(p *panes.Pane) int {
	switch {
	case p.NeedsYou:
		return 0
	case p.IsAgent:
		return 1
	}
	return 2
}

// panePreview is the head for a list of panes and the capture of the first.
func panePreview(head []string, ps []*panes.Pane) previewSpec {
	spec := previewSpec{Head: head}
	for i, p := range ps {
		if i == 3 {
			spec.Head = append(spec.Head, fmt.Sprintf("… and %d more", len(ps)-3))
			break
		}
		mark := "  "
		if i == 0 {
			mark = "▸ "
		}
		spec.Head = append(spec.Head, mark+paneLine(p))
	}
	if len(ps) > 0 {
		p := ps[0]
		spec.Key, spec.Session, spec.Window = p.Session+"/"+p.ID, p.Session, p.ID
		if p.Cwd != "" {
			spec.Head = append(spec.Head, "cwd "+p.Cwd)
		}
	}
	return spec
}

// taskPreview is a task option's preview: its path and status, the panes
// assigned to it, and the first one's recent output.
func (m *Model) taskPreview(opt string) previewSpec {
	t, _ := m.findTask(opt)
	if t == nil {
		return previewSpec{}
	}
	ps := m.relevantPanes(t, nil)
	head := []string{m.taskPath(*t), "status " + t.Status}
	if len(ps) == 0 {
		return previewSpec{Head: append(head, "no live pane is assigned to this task")}
	}
	return panePreview(head, ps)
}

// projectPreview is a project option's preview, like taskPreview's.
func (m *Model) projectPreview(opt string) previewSpec {
	p := m.store.ProjectByName(strings.TrimSpace(opt))
	if p == nil {
		return previewSpec{}
	}
	ps := m.relevantPanes(nil, p)
	head := []string{p.Name, "status " + p.Status}
	if len(ps) == 0 {
		return previewSpec{Head: append(head, "no live pane is assigned to this project")}
	}
	return panePreview(head, ps)
}

// parentPreview is a parent field's preview: a project's top-level entry
// previews the project, any other entry the task.
func (m *Model) parentPreview(opt string) previewSpec {
	for _, p := range m.store.Projects() {
		if opt == topLevel(p.Name) {
			return m.projectPreview(p.Name)
		}
	}
	return m.taskPreview(opt)
}

// windowPreview is a window option's preview: the pane's metadata, its
// assignment, and its recent output. session narrows the match.
func (m *Model) windowPreview(session string) func(string) previewSpec {
	return func(opt string) previewSpec {
		opt = strings.TrimSpace(opt)
		for _, s := range m.merged {
			if session != "" && s.Name != session {
				continue
			}
			for i := range s.Panes {
				p := &s.Panes[i]
				if p.ID != opt && p.Name != opt {
					continue
				}
				head := []string{paneLine(p)}
				if p.Cwd != "" {
					head = append(head, "cwd "+p.Cwd)
				}
				if p.Message != "" {
					head = append(head, p.Message)
				}
				if a, ok := m.assignmentOf(p.ID); ok {
					head = append(head, "assigned to "+a.path)
				}
				return previewSpec{Head: head, Key: p.Session + "/" + p.ID, Session: p.Session, Window: p.ID}
			}
		}
		return previewSpec{}
	}
}
