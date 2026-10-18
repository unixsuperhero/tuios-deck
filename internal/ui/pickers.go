package ui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/unixsuperhero/tuios-deck/internal/store"
)

// The prototypes page holds several takes on the assign and filter
// pickers so the person can try each one on the real store and panes and
// pick a winner later. Nothing here records which one they liked.

// purpose is what a picker's choice does: assign the pane selected on the
// deck page, or filter the context page.
type purpose int

const (
	purposeAssign purpose = iota
	purposeFilter
)

func (p purpose) String() string { return [...]string{"assign", "filter"}[p] }

// pnode is one thing a picker offers. In the assign purpose it is a
// project or a task; in the filter purpose it starts with a context kind
// ("any" for every kind) and may go on to a project and a task, and may
// carry a text to match. New is the "+ add new…" entry, with what was
// typed in Text.
type pnode struct {
	Kind    string
	Project *store.Project
	Task    *store.Task
	New     bool
	Text    string
}

// picker is one prototype. open readies it for a purpose; handleKey takes
// every key while it is up; view draws it as a box width wide and at most
// height tall; done reports, once, that it finished and with what: a nil
// node is a cancel.
type picker interface {
	open(m *Model, p purpose)
	handleKey(msg tea.KeyPressMsg) tea.Cmd
	view(width, height int) []string
	done() (bool, *pnode)
}

// pickBase is what every prototype shares: the model, the purpose and the
// finish it has not reported yet.
type pickBase struct {
	m        *Model
	purpose  purpose
	finished bool
	pick     *pnode
}

func (b *pickBase) init(m *Model, p purpose) { b.m, b.purpose = m, p }

func (b *pickBase) finish(n *pnode) { b.finished, b.pick = true, n }

func (b *pickBase) done() (bool, *pnode) {
	f, n := b.finished, b.pick
	b.finished, b.pick = false, nil
	return f, n
}

// protoDef is a row of the prototypes page.
type protoDef struct {
	name    string
	desc    string
	purpose purpose
	keys    []string
	make    func() picker
}

func protoDefs() []protoDef {
	preview := "? preview: the target's details and its pane's recent screen"
	fuzzyKeys := []string{"type to filter, fuzzy, the matched letters marked", "ctrl+n/ctrl+p or ↓/↑ move", "enter pick", preview, "esc cancel"}
	twoKeys := append([]string{"the right side previews the highlighted target"}, fuzzyKeys...)
	treeKeys := []string{"j/k or ↓/↑ move · g/G top/bottom", "l/→ expand, again to step in", "h/← collapse, again to the parent", "/ filter: keeps matching branches, esc clears it", "enter pick the node", preview, "esc cancel"}
	millerKeys := []string{"j/k or ↓/↑ move in the column", "l/→ next column · h/← previous", "the breadcrumb shows the path", "enter pick the highlighted node at any depth", preview, "esc cancel"}
	chipKeys := []string{"type a name; / locks it as a chip", "tab locks the highlighted completion", "backspace on an empty segment pops the chip", "ctrl+n/ctrl+p or ↓/↑ move in the completions", "enter pick what the chips spell", preview, "esc cancel"}
	hotKeys := []string{"1-9 pick that target at once", "0 more…: the single fuzzy list", "j/k move · enter pick", "esc cancel"}
	defs := []protoDef{
		{"single fuzzy list", "one query line, a tall ranked list of every target", purposeAssign, append(fuzzyKeys, "+ add new… opens the mass-add screen"), func() picker { return &fuzzyPicker{} }},
		{"two-pane preview", "the list left, the highlighted target's details and screen right", purposeAssign, twoKeys, func() picker { return &fuzzyPicker{twoPane: true} }},
		{"tree", "projects collapsed, expand and filter by branch", purposeAssign, treeKeys, func() picker { return &treePicker{} }},
		{"hotkey quick-pick", "the 9 most used and recent targets on digits", purposeAssign, hotKeys, func() picker { return &hotkeyPicker{} }},
		{"miller columns", "project, task and subtask columns under a breadcrumb", purposeAssign, millerKeys, func() picker { return &millerPicker{} }},
		{"type-ahead chips", "one input; deck/ locks the project, then tasks complete", purposeAssign, chipKeys, func() picker { return &chipsPicker{} }},
	}
	filters := map[string]struct {
		desc string
		keys []string
	}{
		"single fuzzy list": {"one query line over every kind / project / task, or a text", append(fuzzyKeys, `text "…" at the end matches the typed text`)},
		"two-pane preview":  {"the list left, the items the filter keeps right", append(twoKeys, `text "…" at the end matches the typed text`)},
		"tree":              {"the kinds as a tree, projects and tasks under each", treeKeys},
		"miller columns":    {"kind, project and task columns under a breadcrumb", millerKeys},
		"type-ahead chips":  {"one input; link/ locks the kind, then projects complete", append(chipKeys, "a last segment naming nothing filters by text")},
	}
	for _, d := range defs[:] {
		f, ok := filters[d.name]
		if !ok {
			continue
		}
		d.purpose, d.desc, d.keys = purposeFilter, f.desc, f.keys
		defs = append(defs, d)
	}
	return defs
}

// protoRun is the prototype that is up, and the pane an assign is for.
type protoRun struct {
	title             string
	purpose           purpose
	windowID, session string
	name              string
}

// openProtos shows the prototypes page.
func (m *Model) openProtos() {
	m.page = pageProtos
	if n := len(protoDefs()); m.protoSel >= n {
		m.protoSel = n - 1
	}
}

func (m *Model) handleProtosKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	n := len(protoDefs())
	switch msg.String() {
	case "esc":
		m.page = pageDeck
		return m, m.loadDetail(true)
	case "j", "down", "ctrl+n":
		if m.protoSel+1 < n {
			m.protoSel++
		}
	case "k", "up", "ctrl+p":
		if m.protoSel > 0 {
			m.protoSel--
		}
	case "g", "home":
		m.protoSel = 0
	case "G", "end":
		m.protoSel = n - 1
	case "enter":
		return m, m.launchProto(m.protoSel)
	}
	return m, nil
}

// launchProto opens prototype i for real: an assign is for the pane
// selected on the deck page.
func (m *Model) launchProto(i int) tea.Cmd {
	defs := protoDefs()
	if i < 0 || i >= len(defs) {
		return nil
	}
	d := defs[i]
	run := protoRun{purpose: d.purpose, title: "filter context · " + d.name}
	if d.purpose == purposeAssign {
		p := m.selected()
		if p == nil {
			m.setStatus("select a pane on the deck page first")
			return nil
		}
		run.windowID, run.session, run.name = p.ID, p.Session, p.Name
		run.title = "assign " + p.Name + " · " + d.name
	}
	m.protoRun = run
	m.protoShots = map[string]*shot{}
	m.proto = d.make()
	m.proto.open(m, d.purpose)
	m.mode = modeProto
	return m.protoAfterKey()
}

func (m *Model) handleProtoKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "ctrl+c" {
		m.mode, m.proto = modeNormal, nil
		m.setStatus("cancelled")
		return m, nil
	}
	cmd := m.proto.handleKey(msg)
	if fin, n := m.proto.done(); fin {
		return m, tea.Batch(cmd, m.finishProto(n))
	}
	return m, tea.Batch(cmd, m.protoAfterKey())
}

// protoPaste types a paste into the picker one character at a time.
func (m *Model) protoPaste(text string) tea.Cmd {
	for _, r := range text {
		m.proto.handleKey(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	return m.protoAfterKey()
}

// protoAfterKey asks for the screen a picker's preview wants, if any.
func (m *Model) protoAfterKey() tea.Cmd {
	if pv, ok := m.proto.(interface{ previewing() *pnode }); ok {
		return m.previewFetch(pv.previewing())
	}
	return nil
}

// finishProto does what the picker picked: assigns the pane or filters the
// context page and goes there. "+ add new…" puts the picker aside while
// the mass-add screen is up; it comes back when that screen closes.
func (m *Model) finishProto(n *pnode) tea.Cmd {
	p := m.proto
	m.mode, m.proto = modeNormal, nil
	run := m.protoRun
	switch {
	case n == nil:
		m.setStatus("cancelled")
		return nil
	case n.New:
		m.protoBack = p
		return m.openMassAdd(m.protoPrefill(n.Text))
	case run.purpose == purposeAssign:
		taskID, projectID := "", ""
		if n.Task != nil {
			taskID = n.Task.ID
		} else if n.Project != nil {
			projectID = n.Project.ID
		}
		if taskID == "" && projectID == "" {
			m.setStatus("nothing to assign to")
			return nil
		}
		return m.assignPane(run.windowID, run.session, run.name, taskID, projectID)
	}
	m.cx.filter = nodeFilter(*n)
	m.openContext()
	if m.cx.filter.empty() {
		m.setStatus("filter cleared")
	} else {
		m.setStatus("filtered: " + m.ctxFilterText())
	}
	return nil
}

// resumeProto brings back the picker the mass-add screen put aside, once
// that screen has closed.
func (m *Model) resumeProto() {
	if m.protoBack != nil && m.form == nil {
		m.proto, m.protoBack = m.protoBack, nil
		m.mode = modeProto
	}
}

// protoPrefill is the outline "+ add new…" merges into the mass-add
// screen: what was typed, as a task under the selected pane's task or
// project, or as a project when there is no project at all.
func (m *Model) protoPrefill(typed string) string {
	return m.taskPrefill(typed)
}

// nodeFilter is the context filter a node spells. A task implies its
// project, so only the task is kept then.
func nodeFilter(n pnode) ctxFilter {
	f := ctxFilter{Text: n.Text}
	if n.Kind != "any" {
		f.Kind = n.Kind
	}
	if n.Task != nil {
		f.TaskID = n.Task.ID
	} else if n.Project != nil {
		f.ProjectID = n.Project.ID
	}
	return f
}

// pickRoots is the first level of the targets: the kinds, "any" first, in
// the filter purpose; the projects in the assign purpose.
func (m *Model) pickRoots(p purpose) []pnode {
	var out []pnode
	if p == purposeFilter {
		for _, k := range append([]string{"any"}, store.ContextKinds()...) {
			out = append(out, pnode{Kind: k})
		}
		return out
	}
	for _, pr := range m.store.Projects() {
		pr := pr
		out = append(out, pnode{Project: &pr})
	}
	return out
}

// pickChildren is the level under a node: a kind's projects, a project's
// top tasks, a task's subtasks.
func (m *Model) pickChildren(n pnode) []pnode {
	var out []pnode
	switch {
	case n.New || n.Text != "":
	case n.Task != nil:
		for _, t := range m.store.Children(n.Task.ID) {
			t := t
			out = append(out, pnode{Kind: n.Kind, Project: n.Project, Task: &t})
		}
	case n.Project != nil:
		for _, t := range m.store.Roots(n.Project.ID) {
			t := t
			out = append(out, pnode{Kind: n.Kind, Project: n.Project, Task: &t})
		}
	case n.Kind != "":
		for _, pr := range m.store.Projects() {
			pr := pr
			out = append(out, pnode{Kind: n.Kind, Project: &pr})
		}
	}
	return out
}

// pickAll is every target in tree order.
func (m *Model) pickAll(p purpose) []pnode {
	var out []pnode
	var walk func(ns []pnode)
	walk = func(ns []pnode) {
		for _, n := range ns {
			out = append(out, n)
			walk(m.pickChildren(n))
		}
	}
	walk(m.pickRoots(p))
	return out
}

// nodeKey names a node for keeping a selection across rebuilds.
func nodeKey(n pnode) string {
	k := n.Kind
	if n.Project != nil {
		k += "|" + n.Project.ID
	}
	if n.Task != nil {
		k += "|" + n.Task.ID
	}
	return k
}

// nodePath is the node in words, "kind / project / task / subtask".
func (m *Model) nodePath(n pnode) string {
	if n.New {
		return addNew
	}
	var parts []string
	if n.Kind != "" {
		parts = append(parts, n.Kind)
	}
	if n.Task != nil {
		parts = append(parts, m.taskPath(*n.Task))
	} else if n.Project != nil {
		parts = append(parts, n.Project.Name)
	}
	s := strings.Join(parts, " / ")
	if n.Text != "" {
		if s != "" {
			s += " / "
		}
		s += `text "` + n.Text + `"`
	}
	return s
}

// nodeName is the node's own name, its last segment.
func nodeName(n pnode) string {
	switch {
	case n.New:
		return addNew
	case n.Task != nil:
		return n.Task.Title
	case n.Project != nil:
		return n.Project.Name
	}
	return n.Kind
}

// nodeGlyph is the node's glyph in its theme colour.
func nodeGlyph(n pnode) string {
	switch {
	case n.New:
		return cStatus.Render("+")
	case n.Text != "" && n.Task == nil && n.Project == nil:
		return cStatus.Render(`"`)
	case n.Task != nil:
		return themeStyle(n.Task.Theme).Render(orDot(n.Task.Theme.Glyph))
	case n.Project != nil:
		return themeStyle(n.Project.Theme).Render(orDot(n.Project.Theme.Glyph))
	case n.Kind == "any":
		return cDim.Render("*")
	}
	return kindStyle(n.Kind).Render("■")
}

func orDot(g string) string {
	if g == "" {
		return "●"
	}
	return g
}

// nodeStatus is a task's or a project's status mark, dim, or "".
func nodeStatus(n pnode) string {
	st := ""
	if n.Task != nil {
		st = n.Task.Status
	} else if n.Project != nil {
		st = n.Project.Status
	}
	if store.IsFinal(st) {
		return " " + cDim.Render(statusMark(st)+" "+st)
	}
	return ""
}

// scored is a node the query kept, how well, and which runes of its path
// matched.
type scored struct {
	n    pnode
	path string
	rank int
	hits map[int]bool
}

// fuzzyNodes ranks the nodes against the query the way the palette does,
// best first and in tree order within a rank, and marks the runes that
// matched. An empty query keeps every node.
func (m *Model) fuzzyNodes(nodes []pnode, query string, name func(pnode) string) []scored {
	q := strings.ToLower(strings.TrimSpace(query))
	var out []scored
	for _, n := range nodes {
		text := name(n)
		if q == "" {
			out = append(out, scored{n: n, path: text})
			continue
		}
		lower := strings.ToLower(text)
		r := matchRank(lower, q)
		if r < 0 {
			continue
		}
		out = append(out, scored{n: n, path: text, rank: r, hits: matchHits(lower, q)})
	}
	sortStable(out)
	return out
}

func sortStable(s []scored) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j].rank < s[j-1].rank; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

// matchHits is which runes of name the query matched: the query where it
// appears whole, else its letters in order, else each of its words.
func matchHits(name, q string) map[int]bool {
	hits := map[int]bool{}
	rs := []rune(name)
	mark := func(w string) {
		wr := []rune(w)
		if i := runeIndex(rs, wr); i >= 0 {
			for j := range wr {
				hits[i+j] = true
			}
			return
		}
		k := 0
		for i, r := range rs {
			if k < len(wr) && wr[k] == r {
				hits[i] = true
				k++
			}
		}
	}
	if strings.Contains(name, q) || subsequence(name, q) {
		mark(q)
		return hits
	}
	for _, w := range strings.Fields(q) {
		mark(w)
	}
	return hits
}

func runeIndex(s, sub []rune) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		ok := true
		for j := range sub {
			if s[i+j] != sub[j] {
				ok = false
				break
			}
		}
		if ok {
			return i
		}
	}
	return -1
}

var cHit = lipgloss.NewStyle().Bold(true).Underline(true).Foreground(lipgloss.Color("3"))

// hlText draws text with the hit runes marked, reversed when selected.
func hlText(text string, hits map[int]bool, sel bool) string {
	plain, hit := lipgloss.NewStyle(), cHit
	if sel {
		plain, hit = plain.Reverse(true), hit.Reverse(true)
	}
	if len(hits) == 0 {
		return plain.Render(text)
	}
	var b strings.Builder
	run, on := "", false
	flush := func() {
		if run == "" {
			return
		}
		if on {
			b.WriteString(hit.Render(run))
		} else {
			b.WriteString(plain.Render(run))
		}
		run = ""
	}
	for i, r := range []rune(text) {
		if hits[i] != on {
			flush()
			on = hits[i]
		}
		run += string(r)
	}
	flush()
	return b.String()
}

// nodeRow is a node's line in a list: its glyph and its path with the
// matched runes marked, padded to width and reversed when selected.
func nodeRow(s scored, width int, sel bool) string {
	text := ansi.Truncate(s.path, width-3, "…")
	line := " " + nodeGlyph(s.n) + " " + hlText(text, s.hits, sel) + nodeStatus(s.n)
	if sel {
		fill := width - ansi.StringWidth(line)
		if fill > 0 {
			line += cSel.Render(strings.Repeat(" ", fill))
		}
	}
	return line
}

// editText applies a typing key to s and reports whether it was one.
func editText(s *string, msg tea.KeyPressMsg) bool {
	switch msg.String() {
	case "backspace":
		if r := []rune(*s); len(r) > 0 {
			*s = string(r[:len(r)-1])
		}
	case "ctrl+u":
		*s = ""
	case "ctrl+w":
		t := strings.TrimRight(*s, " /")
		if i := strings.LastIndexAny(t, " /"); i >= 0 {
			*s = t[:i+1]
		} else {
			*s = ""
		}
	default:
		k := msg.Key()
		if k.Text == "" || k.Mod != 0 {
			return false
		}
		*s += k.Text
	}
	return true
}

// moveSel steps a highlight through n entries for the list keys and
// reports whether the key was one of them.
func moveSel(sel *int, n int, key string) bool {
	switch key {
	case "ctrl+n", "down":
		if *sel+1 < n {
			*sel++
		}
	case "ctrl+p", "up":
		if *sel > 0 {
			*sel--
		}
	default:
		return false
	}
	return true
}

// window is the start of a scrolled list of n entries showing rows of
// them with sel in sight.
func window(sel, rows, n int) int {
	start := 0
	if sel >= rows {
		start = sel - rows + 1
	}
	if start > n-rows && n > rows {
		start = n - rows
	}
	if start < 0 {
		start = 0
	}
	return start
}

// protoWidth is a prototype box's width on a screen w wide.
func protoWidth(w int) int {
	bw := w - 2
	if bw > 120 {
		bw = 120
	}
	if bw < 20 {
		bw = 20
	}
	return bw
}

// pickerBox draws a prototype: the title in the top border, the body, a
// rule and the keys at the bottom.
func pickerBox(title string, body []string, help string, width int) []string {
	iw := width - 4
	lines := []string{boxTop(title, width)}
	for _, l := range body {
		lines = append(lines, boxRow(ansi.Truncate(l, iw, ""), iw))
	}
	return append(lines,
		cBorder.Render("├"+strings.Repeat("─", width-2)+"┤"),
		boxRow(cDim.Render(ansi.Truncate(help, iw, "…")), iw),
		cBorder.Render("╰"+strings.Repeat("─", width-2)+"╯"),
	)
}

// protoListLines draws the prototypes page's rows: the number, the name,
// what it drives and its description.
func (m *Model) protoListLines(width, height int) []string {
	lines := make([]string, 0, height)
	defs := protoDefs()
	start := window(m.protoSel, height, len(defs))
	for i := start; i < len(defs) && len(lines) < height; i++ {
		d := defs[i]
		num := (i % 6) + 1
		if d.purpose == purposeFilter {
			num = map[string]int{"single fuzzy list": 1, "two-pane preview": 2, "tree": 3, "miller columns": 5, "type-ahead chips": 6}[d.name]
		}
		drive := cStatus.Render(pad(d.purpose.String(), 6))
		if d.purpose == purposeFilter {
			drive = cPin.Render(pad(d.purpose.String(), 6))
		}
		line := fmt.Sprintf(" %d %s %s ", num, pad(d.name, 17), drive) + cDim.Render(d.desc)
		if i == m.protoSel {
			line = cSel.Render(ansi.Strip(pad(line, width)))
		}
		lines = append(lines, ansi.Truncate(line, width, "…"))
	}
	for len(lines) < height {
		lines = append(lines, "")
	}
	return lines
}

// protoDetailLines describes the highlighted prototype: what it drives,
// on what, and its keys.
func (m *Model) protoDetailLines(width, height int) []string {
	defs := protoDefs()
	var lines []string
	if m.protoSel < len(defs) {
		d := defs[m.protoSel]
		lines = append(lines, cTitle.Render(d.name)+"  "+cDim.Render(d.purpose.String()))
		lines = append(lines, d.desc, "")
		if d.purpose == purposeAssign {
			target := cErr.Render("no pane selected on the deck page")
			if p := m.selected(); p != nil {
				target = p.Session + " › " + p.Name
			}
			lines = append(lines, cDim.Render("enter assigns ")+target)
		} else {
			lines = append(lines, cDim.Render("enter filters the context page and goes there"))
		}
		lines = append(lines, "", cHeader.Render("keys"))
		for _, k := range d.keys {
			lines = append(lines, "  "+k)
		}
		lines = append(lines, "", cDim.Render("These are prototypes to try; one will replace the forms."))
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
