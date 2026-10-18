package ui

import (
	"fmt"
	"sort"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/unixsuperhero/tuios-deck/internal/store"
)

// hotkeyPicker is prototype 4: the nine most used and most recent targets
// on the digits 1 to 9, and 0 for the single fuzzy list.
type hotkeyPicker struct {
	pickBase
	sel  int
	more *fuzzyPicker
}

type hotItem struct {
	n    pnode
	used int
	at   int64
}

func (h *hotkeyPicker) open(m *Model, p purpose) { h.init(m, p) }

// items is up to nine targets: the ones panes are assigned to, most panes
// first, then by the time their task last changed, then the rest of the
// open tasks and the projects by that time.
func (h *hotkeyPicker) items() []hotItem {
	used := map[string]int{}
	for _, a := range h.m.store.Assignments() {
		if a.TaskID != "" {
			used["t"+a.TaskID]++
		} else if a.ProjectID != "" {
			used["p"+a.ProjectID]++
		}
	}
	var all []hotItem
	latest := map[string]int64{}
	for _, t := range h.m.store.Tasks() {
		pid := h.m.store.ProjectOf(t.ID)
		if t.Updated > latest[pid] {
			latest[pid] = t.Updated
		}
		if store.IsFinal(t.Status) && used["t"+t.ID] == 0 {
			continue
		}
		t := t
		n := pnode{Task: &t, Project: h.m.store.Project(pid)}
		all = append(all, hotItem{n: n, used: used["t"+t.ID], at: t.Updated})
	}
	for _, p := range h.m.store.Projects() {
		if store.IsFinal(p.Status) && used["p"+p.ID] == 0 {
			continue
		}
		p := p
		at := latest[p.ID]
		if at == 0 {
			at = p.Created
		}
		all = append(all, hotItem{n: pnode{Project: &p}, used: used["p"+p.ID], at: at})
	}
	sort.SliceStable(all, func(i, j int) bool {
		if all[i].used != all[j].used {
			return all[i].used > all[j].used
		}
		return all[i].at > all[j].at
	})
	if len(all) > 9 {
		all = all[:9]
	}
	return all
}

func (h *hotkeyPicker) done() (bool, *pnode) {
	if h.more != nil {
		return h.more.done()
	}
	return h.pickBase.done()
}

func (h *hotkeyPicker) previewing() *pnode {
	if h.more != nil {
		return h.more.previewing()
	}
	return nil
}

func (h *hotkeyPicker) handleKey(msg tea.KeyPressMsg) tea.Cmd {
	if h.more != nil {
		return h.more.handleKey(msg)
	}
	items := h.items()
	key := msg.String()
	switch {
	case key == "esc":
		h.finish(nil)
	case key == "0":
		h.more = &fuzzyPicker{}
		h.more.open(h.m, h.purpose)
	case len(key) == 1 && key[0] >= '1' && key[0] <= '9':
		if i := int(key[0] - '1'); i < len(items) {
			n := items[i].n
			h.finish(&n)
		}
	case key == "enter":
		if h.sel < len(items) {
			n := items[h.sel].n
			h.finish(&n)
		}
	case key == "j":
		moveSel(&h.sel, len(items), "down")
	case key == "k":
		moveSel(&h.sel, len(items), "up")
	default:
		moveSel(&h.sel, len(items), key)
	}
	return nil
}

func (h *hotkeyPicker) view(width, height int) []string {
	if h.more != nil {
		return h.more.view(width, height)
	}
	iw := width - 4
	items := h.items()
	var body []string
	if len(items) == 0 {
		body = append(body, cDim.Render(" no projects or tasks yet: 0 for the list, where + add new… makes one"))
	}
	for i, it := range items {
		info := "updated " + ago(it.at)
		if it.used > 0 {
			info = plural(it.used, "pane") + " · " + info
		}
		info = cDim.Render(info)
		key := cStatus.Render(fmt.Sprintf(" %d ", i+1))
		path := ansi.Truncate(h.m.nodePath(it.n), iw-ansi.StringWidth(info)-8, "…")
		line := key + nodeGlyph(it.n) + " " + path
		line = pad(line, iw-ansi.StringWidth(info)) + info
		if i == h.sel {
			line = cSel.Render(ansi.Strip(line))
		}
		body = append(body, line)
	}
	body = append(body, "", cStatus.Render(" 0 ")+"  more… "+cDim.Render("every target in the single fuzzy list"))
	return pickerBox(h.m.protoRun.title, body, width)
}
