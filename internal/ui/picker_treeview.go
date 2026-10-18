package ui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/unixsuperhero/tuios-deck/internal/panes"
	"github.com/unixsuperhero/tuios-deck/internal/store"
)

// treeView is the assign modal and the context filter, the winner of the
// prototypes: the targets as a tree on the left, every branch open, and
// the highlighted target's preview on the right. Typing filters the tree;
// until something is typed the letter keys move. In the filter purpose
// the kinds come first, every row counts its context items, a row with
// none is hidden, and the query also matches the items' text.
type treeView struct {
	pickBase
	closed map[string]bool
	sel    int
	selKey string
	query  string
	// typing is set by / so a query can start with a key that moves.
	typing bool
	narrow bool
}

// tvRow is a line of the tree: the node, how deep, whether it has
// children shown or hidden and whether they are shown, and the runes of
// its name the query matched. Path says the query matched only the path;
// items, in the filter purpose, that it matched only the items' text.
type tvRow struct {
	n      pnode
	depth  int
	kids   bool
	opened bool
	hits   map[int]bool
	path   bool
	items  bool
	count  int
}

func (t *treeView) open(m *Model, p purpose) {
	t.init(m, p)
	t.closed = map[string]bool{}
}

// matchNode says whether the query keeps the node by its name or, failing
// that, by its path, and which runes of the name matched.
func (t *treeView) matchNode(n pnode, q string) (name bool, path bool, hits map[int]bool) {
	lower := strings.ToLower(tvName(n))
	if matchRank(lower, q) >= 0 {
		return true, false, matchHits(lower, q)
	}
	if t.purpose == purposeAssign && matchRank(strings.ToLower(t.m.nodePath(n)), q) >= 0 {
		return false, true, nil
	}
	return false, false, nil
}

// tvName is a node's name in the tree; the every-kind row is "all".
func tvName(n pnode) string {
	if n.Kind == "any" {
		return "all"
	}
	return nodeName(n)
}

// itemsOf is the context items from all that a node holds: every item
// for "all", a kind's items, a project's own and its tasks', a task's
// and its subtasks'.
func (t *treeView) itemsOf(n pnode, all []store.ContextItem) []store.ContextItem {
	var under map[string]bool
	if n.Task != nil {
		under = t.m.subtree(n.Task.ID)
	}
	var out []store.ContextItem
	for _, c := range all {
		switch {
		case n.Task != nil:
			if !under[c.TaskID] {
				continue
			}
		case n.Project != nil:
			if c.ProjectID != n.Project.ID {
				continue
			}
		case n.Kind != "any" && c.Kind != n.Kind:
			continue
		}
		out = append(out, c)
	}
	return out
}

// anyHas says whether one of the items' text holds q, ignoring case.
func anyHas(items []store.ContextItem, q string) bool {
	for _, c := range items {
		if strings.Contains(strings.ToLower(c.Text), q) {
			return true
		}
	}
	return false
}

// rows is the tree as drawn. Without a query every branch is open unless
// the person closed it. With one a node shows when it or a node under it
// matches, and every branch shown is open. "+ add new…" is always last
// when assigning. When filtering, "all" and the kinds come first, a node
// with no items is left out, and a node also matches by its items' text.
func (t *treeView) rows() []tvRow {
	q := strings.ToLower(strings.TrimSpace(t.query))
	filter := t.purpose == purposeFilter
	var all []store.ContextItem
	var out []tvRow
	if filter {
		all = t.m.scopedItems()
		for _, k := range append([]string{"any"}, store.ContextKinds()...) {
			n := pnode{Kind: k}
			items := t.itemsOf(n, all)
			name, _, hits := t.matchNode(n, q)
			ih := q != "" && anyHas(items, q)
			if k != "any" && (len(items) == 0 || q != "" && !name && !ih) {
				continue
			}
			out = append(out, tvRow{n: n, hits: hits, items: ih && !name, count: len(items)})
		}
	}
	var walk func(ns []pnode, depth int) bool
	walk = func(ns []pnode, depth int) bool {
		kept := false
		for _, n := range ns {
			var items []store.ContextItem
			if filter {
				if items = t.itemsOf(n, all); len(items) == 0 {
					continue
				}
			}
			kids := t.m.pickChildren(n)
			if filter {
				var held []pnode
				for _, k := range kids {
					if len(t.itemsOf(k, all)) > 0 {
						held = append(held, k)
					}
				}
				kids = held
			}
			row := tvRow{n: n, depth: depth, kids: len(kids) > 0, count: len(items)}
			if q == "" {
				row.opened = row.kids && !t.closed[nodeKey(n)]
				out = append(out, row)
				if row.opened {
					walk(kids, depth+1)
				}
				kept = true
				continue
			}
			at := len(out)
			out = append(out, row)
			name, path, hits := t.matchNode(n, q)
			ih := filter && anyHas(items, q)
			under := walk(kids, depth+1)
			if !name && !path && !ih && !under {
				out = out[:at]
				continue
			}
			out[at].hits, out[at].path, out[at].opened = hits, path && !name, under
			out[at].items = ih && !name
			kept = true
		}
		return kept
	}
	walk(t.m.pickRoots(purposeAssign), 0)
	if filter {
		return out
	}
	last := pnode{New: true, Text: strings.TrimSpace(t.query)}
	if t.purpose == purposeActive {
		last.Kind = "project" // "+ new project…": the new project form, not the mass-add screen
	}
	return append(out, tvRow{n: last})
}

// place keeps the selection on the same node after the rows change.
func (t *treeView) place(rows []tvRow) {
	if t.selKey == "\x00new" {
		t.sel = len(rows) - 1
		return
	}
	for i, r := range rows {
		if nodeKey(r.n) == t.selKey && !r.n.New {
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

func (t *treeView) remember(rows []tvRow) {
	if t.sel >= 0 && t.sel < len(rows) {
		t.selKey = nodeKey(rows[t.sel].n)
		if rows[t.sel].n.New {
			t.selKey = "\x00new"
		}
	}
}

func (t *treeView) current() *pnode {
	rows := t.rows()
	t.place(rows)
	if t.sel < len(rows) {
		return &rows[t.sel].n
	}
	return nil
}

// previewing is the node whose pane's screen the preview wants, none when
// the box is too narrow to show a screen.
func (t *treeView) previewing() *pnode {
	if t.narrow || t.purpose == purposeFilter {
		return nil
	}
	return t.current()
}

// firstMatch puts the selection on the first row whose name the query
// matched, else the first whose path or items did, else the last row:
// "+ add new…" when assigning.
func (t *treeView) firstMatch() {
	rows := t.rows()
	t.sel = len(rows) - 1
	for i, r := range rows {
		if len(r.hits) > 0 {
			t.sel = i
			return
		}
	}
	for i, r := range rows {
		if r.path || r.items && r.n.Kind != "any" {
			t.sel = i
			return
		}
	}
}

// selectKey puts the selection on the node with that key, if it shows.
func (t *treeView) selectKey(key string) {
	t.selKey = key
	t.place(t.rows())
}

func (t *treeView) handleKey(msg tea.KeyPressMsg) tea.Cmd {
	rows := t.rows()
	t.place(rows)
	defer func() { t.remember(t.rows()) }()
	key := msg.String()
	var cur *tvRow
	if t.sel < len(rows) {
		cur = &rows[t.sel]
	}
	switch key {
	case "esc":
		t.finish(nil)
		return nil
	case "enter":
		if cur != nil {
			n := cur.n
			if q := strings.TrimSpace(t.query); t.purpose == purposeFilter && q != "" && matchRank(strings.ToLower(tvName(n)), strings.ToLower(q)) < 0 {
				n.Text = q
			}
			t.finish(&n)
		}
		return nil
	case "home":
		t.sel = 0
		return nil
	case "end":
		t.sel = len(rows) - 1
		return nil
	case "left":
		t.collapse(rows, cur)
		return nil
	case "right":
		t.expand(cur)
		return nil
	case "backspace":
		if t.query == "" {
			t.typing = false
			return nil
		}
		editText(&t.query, msg)
		t.firstMatch()
		return nil
	}
	if moveSel(&t.sel, len(rows), key) {
		return nil
	}
	if !t.typing && t.query == "" {
		switch key {
		case "/":
			t.typing = true
			return nil
		case "j":
			moveSel(&t.sel, len(rows), "down")
			return nil
		case "k":
			moveSel(&t.sel, len(rows), "up")
			return nil
		case "g":
			t.sel = 0
			return nil
		case "G":
			t.sel = len(rows) - 1
			return nil
		case "h":
			t.collapse(rows, cur)
			return nil
		case "l":
			t.expand(cur)
			return nil
		case "{", "}":
			t.sibling(rows, map[string]int{"{": -1, "}": 1}[key])
			return nil
		case "(":
			t.parent(rows)
			return nil
		}
	}
	if editText(&t.query, msg) {
		t.typing = t.query != ""
		t.firstMatch()
	}
	return nil
}

// expand opens the highlighted branch, or steps into it when it is open.
func (t *treeView) expand(cur *tvRow) {
	if cur == nil || !cur.kids {
		return
	}
	if !cur.opened {
		delete(t.closed, nodeKey(cur.n))
		return
	}
	t.sel++
}

// collapse closes the highlighted branch, or steps out to its parent when
// it is closed or a leaf.
func (t *treeView) collapse(rows []tvRow, cur *tvRow) {
	if cur == nil {
		return
	}
	if cur.opened && t.query == "" {
		t.closed[nodeKey(cur.n)] = true
		return
	}
	t.parent(rows)
}

// parent moves to the highlighted node's parent.
func (t *treeView) parent(rows []tvRow) {
	if t.sel >= len(rows) {
		return
	}
	d := rows[t.sel].depth
	for i := t.sel - 1; i >= 0; i-- {
		if rows[i].depth < d {
			t.sel = i
			return
		}
	}
}

// sibling moves to the previous or next node under the same parent.
func (t *treeView) sibling(rows []tvRow, dir int) {
	if t.sel >= len(rows) {
		return
	}
	d := rows[t.sel].depth
	for i := t.sel + dir; i >= 0 && i < len(rows); i += dir {
		if rows[i].n.New || rows[i].depth < d {
			return
		}
		if rows[i].depth == d {
			t.sel = i
			return
		}
	}
}

// tvNarrow is the box width under which the preview drops the screen.
const tvNarrow = 100

func (t *treeView) view(width, height int) []string {
	rows := t.rows()
	t.place(rows)
	t.narrow = width < tvNarrow
	iw := width - 4
	bodyH := height - 4
	if bodyH < 5 {
		bodyH = 5
	}
	listH := bodyH - 2
	listW := iw/2 - 1
	prompt := ">"
	if t.typing && t.query == "" {
		prompt = "/"
	}
	body := []string{" " + cStatus.Render(prompt) + " " + t.query + "▏", cBorder.Render(strings.Repeat("─", iw))}
	var lines []string
	start := window(t.sel, listH, len(rows))
	for i := start; i < len(rows) && len(lines) < listH; i++ {
		lines = append(lines, t.rowLine(rows[i], listW, i == t.sel))
	}
	for len(lines) < listH {
		lines = append(lines, "")
	}
	pv := t.preview(t.current(), iw-listW-2, listH)
	for i := range lines {
		lines[i] = pad(lines[i], listW) + cBorder.Render(" │") + pv[i]
	}
	return pickerBox(t.m.protoRun.title, append(body, lines...), width)
}

// rowLine draws a row: the indent, the branch mark, the glyph and the name
// with the matched runes marked, reversed when selected.
func (t *treeView) rowLine(r tvRow, width int, sel bool) string {
	var line string
	if r.n.New {
		line = " " + hlText(nodeName(r.n), nil, sel)
		if !sel {
			line = " " + cStatus.Render(nodeName(r.n))
		}
	} else {
		mark := "  "
		if r.kids {
			mark = "▸ "
			if r.opened {
				mark = "▾ "
			}
		}
		indent := strings.Repeat("  ", r.depth)
		count := ""
		if t.purpose == purposeFilter {
			count = fmt.Sprint(r.count)
		}
		name := ansi.Truncate(tvName(r.n), width-len(indent)-7-len(count), "…")
		line = indent + cDim.Render(mark) + nodeGlyph(r.n) + " " + hlText(name, r.hits, sel) + nodeStatus(r.n)
		if count != "" {
			gap := width - ansi.StringWidth(line) - len(count) - 1
			if gap < 1 {
				gap = 1
			}
			fill := strings.Repeat(" ", gap)
			if sel {
				fill = cSel.Render(fill)
			}
			line += fill + cDim.Render(count)
		}
	}
	if sel {
		if fill := width - ansi.StringWidth(line); fill > 0 {
			line += cSel.Render(strings.Repeat(" ", fill))
		}
	}
	return ansi.Truncate(line, width, "…")
}

// preview is the right side: the target's details and, when the box is
// wide enough, the recent screen of its first pane.
func (t *treeView) preview(n *pnode, width, height int) []string {
	if t.purpose == purposeFilter {
		return t.filterPreview(n, width, height)
	}
	lines := t.m.previewLines(n, width, height)
	if !t.narrow {
		return lines
	}
	for i, l := range lines {
		if strings.Contains(l, "── screen of") {
			for j := i; j < len(lines); j++ {
				lines[j] = ""
			}
			break
		}
	}
	return lines
}

// filterPreview lists the items the highlighted node would keep: those
// holding the query first, marked, and the rest after them unless the
// query would become a text filter.
func (t *treeView) filterPreview(n *pnode, width, height int) []string {
	var lines []string
	if n == nil {
		lines = append(lines, cDim.Render("nothing matches"))
	} else {
		q := strings.ToLower(strings.TrimSpace(t.query))
		title := "all"
		if n.Kind != "any" {
			title = t.m.nodePath(*n)
		}
		lines = append(lines, nodeGlyph(*n)+" "+cTitle.Render(title))
		items := t.itemsOf(*n, t.m.scopedItems())
		text := q != "" && matchRank(strings.ToLower(tvName(*n)), q) < 0
		var hit, rest []store.ContextItem
		for _, c := range items {
			if q != "" && strings.Contains(strings.ToLower(c.Text), q) {
				hit = append(hit, c)
			} else if !text {
				rest = append(rest, c)
			}
		}
		keep := plural(len(hit)+len(rest), "context item")
		if text {
			keep += ` holding "` + strings.TrimSpace(t.query) + `"`
		}
		lines = append(lines, cDim.Render(pad("keeps", 9))+keep, "")
		for _, c := range hit {
			one := panes.OneLine(c.Text)
			lines = append(lines, kindStyle(c.Kind).Render(pad(c.Kind, 7))+" "+hlText(one, matchHits(strings.ToLower(one), q), false))
		}
		for _, c := range rest {
			lines = append(lines, kindStyle(c.Kind).Render(pad(c.Kind, 7))+" "+panes.OneLine(c.Text))
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

// mergeFilter is the context filter after picking n: "all" clears it, a
// kind row sets the kind, a project or task row sets that, and the text
// is what the pick carries.
func mergeFilter(f ctxFilter, n pnode) ctxFilter {
	switch {
	case n.Kind == "any":
		f = ctxFilter{}
	case n.Task != nil:
		f.TaskID, f.ProjectID = n.Task.ID, ""
	case n.Project != nil:
		f.ProjectID, f.TaskID = n.Project.ID, ""
	default:
		f.Kind = n.Kind
	}
	f.Text = n.Text
	return f
}

// added is told what the mass-add screen made, so the tree comes back
// with the new task, or the new project, highlighted.
func (t *treeView) added(r massResult) {
	t.query, t.typing = "", false
	if r.task != "" {
		if tk, _ := t.m.findTask(r.task); tk != nil {
			t.selectKey("|" + t.m.store.ProjectOf(tk.ID) + "|" + tk.ID)
			return
		}
	}
	if p := t.m.store.ProjectByName(r.project); p != nil {
		t.selectKey("|" + p.ID)
	}
}

// openTreeView puts the tree up for run: the assign of a pane or of a
// context item, highlighting what it is assigned to now, or the context
// filter, highlighting the filter's kind, project or task.
func (m *Model) openTreeView(run protoRun, projectID, taskID string) tea.Cmd {
	m.protoRun = run
	m.protoShots = map[string]*shot{}
	tv := &treeView{}
	tv.open(m, run.purpose)
	switch {
	case taskID != "":
		tv.selectKey("|" + m.store.ProjectOf(taskID) + "|" + taskID)
	case projectID != "":
		tv.selectKey("|" + projectID)
	case run.purpose == purposeFilter && m.cx.filter.Kind != "":
		tv.selectKey(m.cx.filter.Kind)
	case run.purpose == purposeFilter:
		tv.selectKey("any")
	}
	m.proto = tv
	m.mode = modeProto
	return m.protoAfterKey()
}
