package ui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// treePicker is prototype 3: the targets as a tree with the first level
// collapsed. l opens a branch, h closes it, and / filters to the branches
// that hold a match, opened.
type treePicker struct {
	pickBase
	opened    map[string]bool
	sel       int
	selKey    string
	filtering bool
	query     string
	preview   bool
}

type treeRow struct {
	n      pnode
	depth  int
	kids   bool
	opened bool
	hits   map[int]bool
}

func (t *treePicker) open(m *Model, p purpose) {
	t.init(m, p)
	t.opened = map[string]bool{}
}

// rows is the tree as drawn. With a query a node shows when it or a node
// under it matches, and every branch shown is open.
func (t *treePicker) rows() []treeRow {
	q := strings.ToLower(strings.TrimSpace(t.query))
	var out []treeRow
	var walk func(ns []pnode, depth int) bool
	walk = func(ns []pnode, depth int) bool {
		any := false
		for _, n := range ns {
			kids := t.m.pickChildren(n)
			row := treeRow{n: n, depth: depth, kids: len(kids) > 0}
			if q == "" {
				row.opened = row.kids && t.opened[nodeKey(n)]
				out = append(out, row)
				if row.opened {
					walk(kids, depth+1)
				}
				continue
			}
			at := len(out)
			out = append(out, row)
			lower := strings.ToLower(nodeName(n))
			self := matchRank(lower, q) >= 0
			under := walk(kids, depth+1)
			if !self && !under {
				out = out[:at]
				continue
			}
			if self {
				out[at].hits = matchHits(lower, q)
			}
			out[at].opened = under
			any = true
		}
		return any
	}
	walk(t.m.pickRoots(t.purpose), 0)
	return out
}

// place keeps the selection on the same node after the rows change.
func (t *treePicker) place(rows []treeRow) {
	for i, r := range rows {
		if nodeKey(r.n) == t.selKey {
			t.sel = i
			return
		}
	}
	if t.sel >= len(rows) {
		t.sel = len(rows) - 1
	}
	if t.sel < 0 {
		t.sel = 0
	}
	t.remember(rows)
}

func (t *treePicker) remember(rows []treeRow) {
	if t.sel >= 0 && t.sel < len(rows) {
		t.selKey = nodeKey(rows[t.sel].n)
	}
}

func (t *treePicker) current() *pnode {
	rows := t.rows()
	t.place(rows)
	if t.sel < len(rows) {
		return &rows[t.sel].n
	}
	return nil
}

func (t *treePicker) previewing() *pnode {
	if !t.preview {
		return nil
	}
	return t.current()
}

func (t *treePicker) handleKey(msg tea.KeyPressMsg) tea.Cmd {
	rows := t.rows()
	t.place(rows)
	key := msg.String()
	defer func() { t.remember(t.rows()) }()
	if key == "enter" {
		if t.sel < len(rows) {
			n := rows[t.sel].n
			t.finish(&n)
		}
		return nil
	}
	if moveSel(&t.sel, len(rows), key) {
		return nil
	}
	if t.filtering {
		switch key {
		case "esc":
			t.filtering, t.query = false, ""
		case "backspace":
			if t.query == "" {
				t.filtering = false
				return nil
			}
			editText(&t.query, msg)
			t.firstMatch()
		default:
			if editText(&t.query, msg) {
				t.firstMatch()
			}
		}
		return nil
	}
	cur := func() *treeRow {
		if t.sel < len(rows) {
			return &rows[t.sel]
		}
		return nil
	}()
	switch key {
	case "esc":
		t.finish(nil)
	case "j":
		moveSel(&t.sel, len(rows), "down")
	case "k":
		moveSel(&t.sel, len(rows), "up")
	case "g", "home":
		t.sel = 0
	case "G", "end":
		t.sel = len(rows) - 1
	case "/":
		t.filtering = true
	case "l", "right":
		if cur == nil || !cur.kids {
			return nil
		}
		if !cur.opened {
			t.opened[nodeKey(cur.n)] = true
		} else {
			t.sel++
		}
	case "h", "left":
		if cur == nil {
			return nil
		}
		if cur.opened {
			delete(t.opened, nodeKey(cur.n))
			return nil
		}
		for i := t.sel - 1; i >= 0; i-- {
			if rows[i].depth < cur.depth {
				t.sel = i
				break
			}
		}
	}
	return nil
}

// firstMatch puts the selection on the first row the query matched.
func (t *treePicker) firstMatch() {
	for i, r := range t.rows() {
		if len(r.hits) > 0 {
			t.sel = i
			return
		}
	}
	t.sel = 0
}

func (t *treePicker) view(width, height int) []string {
	rows := t.rows()
	t.place(rows)
	iw := width - 4
	bodyH := height - 4
	if bodyH < 3 {
		bodyH = 3
	}
	listH := bodyH
	if t.filtering {
		listH -= 2
	}
	listW := iw
	if t.preview {
		listW = iw/2 - 1
	}
	var lines []string
	start := window(t.sel, listH, len(rows))
	for i := start; i < len(rows) && len(lines) < listH; i++ {
		r := rows[i]
		sel := i == t.sel
		mark := "  "
		if r.kids {
			mark = "▸ "
			if r.opened {
				mark = "▾ "
			}
		}
		indent := strings.Repeat("  ", r.depth)
		name := ansi.Truncate(nodeName(r.n), listW-len(indent)-6, "…")
		line := indent + cDim.Render(mark) + nodeGlyph(r.n) + " " + hlText(name, r.hits, sel) + nodeStatus(r.n)
		if sel {
			if fill := listW - ansi.StringWidth(line); fill > 0 {
				line += cSel.Render(strings.Repeat(" ", fill))
			}
		}
		lines = append(lines, line)
	}
	if len(rows) == 0 {
		lines = append(lines, cDim.Render("   no matches"))
	}
	for len(lines) < listH {
		lines = append(lines, "")
	}
	if t.preview {
		pv := t.m.previewLines(t.current(), iw-listW-2, listH)
		for i := range lines {
			lines[i] = pad(lines[i], listW) + cBorder.Render(" │") + pv[i]
		}
	}
	help := "j/k move · l open · h close · / filter · enter pick · esc cancel"
	if t.filtering {
		lines = append(lines, cBorder.Render(strings.Repeat("─", iw)), " "+cStatus.Render("/")+" "+t.query+"▏")
		help = "type to filter · ↓/↑ move · enter pick · esc clear the filter"
	}
	return pickerBox(t.m.protoRun.title, lines, help, width)
}
