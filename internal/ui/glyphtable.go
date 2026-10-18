package ui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/unixsuperhero/tuios-deck/internal/store"
)

// glyphTable is the glyph picker: the store's glyph sections as a grid
// under a preview of the project or task drawn with the highlighted glyph.
// h/j/k/l and the arrows move, a typed character jumps to its glyph or,
// when the table lacks it, becomes a custom entry at the end, enter picks
// and esc cancels.
type glyphTable struct {
	title   string
	items   []string // every glyph in table order, the custom one last
	labels  []string // the section of each item
	sel     int
	typed   string // what was typed, cut to its last grapheme
	custom  string // a typed glyph the table does not hold
	preview func(glyph string) []string
	done    func(glyph string) tea.Cmd
	back    *form // the form to return to, when the table was opened from one
	backAt  int   // the field of back that takes the glyph
}

// newGlyphTable opens on current, or on a custom entry for it when the
// table does not hold it.
func newGlyphTable(title, current string, preview func(string) []string) *glyphTable {
	g := &glyphTable{title: title, preview: preview}
	for _, s := range store.GlyphSections() {
		for _, gl := range s.Glyphs {
			g.items = append(g.items, gl)
			g.labels = append(g.labels, s.Label)
		}
	}
	if i := g.index(current); i >= 0 {
		g.sel = i
	} else if current != "" {
		g.setCustom(current)
	}
	return g
}

func (g *glyphTable) index(glyph string) int {
	for i, it := range g.items {
		if it == glyph {
			return i
		}
	}
	return -1
}

// setCustom makes glyph the last entry, in a section of its own, and
// selects it.
func (g *glyphTable) setCustom(glyph string) {
	if g.custom != "" {
		g.items, g.labels = g.items[:len(g.items)-1], g.labels[:len(g.labels)-1]
	}
	g.custom = glyph
	g.items, g.labels = append(g.items, glyph), append(g.labels, "custom")
	g.sel = len(g.items) - 1
}

// cellWidth is the columns one cell takes: the widest glyph and a space
// either side, so a wide glyph never pushes the columns out of line.
func (g *glyphTable) cellWidth() int {
	w := 1
	for _, it := range g.items {
		w = max(w, ansi.StringWidth(it))
	}
	return w + 2
}

// layout is the grid's rows for a box iw columns wide: each row the item
// indices it shows, with a section's label row (-1 entries) before it.
func (g *glyphTable) layout(iw int) (rows [][]int, label []string) {
	cols := max(iw/g.cellWidth(), 1)
	for i := 0; i < len(g.items); {
		lab := g.labels[i]
		rows, label = append(rows, nil), append(label, lab)
		row := []int{}
		for ; i < len(g.items) && g.labels[i] == lab; i++ {
			if len(row) == cols {
				rows, label = append(rows, row), append(label, "")
				row = []int{}
			}
			row = append(row, i)
		}
		rows, label = append(rows, row), append(label, "")
	}
	return rows, label
}

// moveRow puts the selection dir grid rows up or down, in the same column
// or the last one of a shorter row. Label rows are passed over.
func (g *glyphTable) moveRow(dir, iw int) {
	rows, _ := g.layout(iw)
	r, c := 0, 0
	for ri, row := range rows {
		for ci, it := range row {
			if it == g.sel {
				r, c = ri, ci
			}
		}
	}
	for next := r + dir; next >= 0 && next < len(rows); next += dir {
		if len(rows[next]) > 0 {
			g.sel = rows[next][min(c, len(rows[next])-1)]
			return
		}
	}
}

// typeText jumps to the glyph the last grapheme typed names: itself, the
// capital of a lower-case letter, or a custom entry for anything else.
func (g *glyphTable) typeText(text string) {
	g.typed += text
	g.typed = g.typed[len(dropLastGrapheme(g.typed)):]
	if i := g.index(g.typed); i >= 0 {
		g.sel = i
		return
	}
	if i := g.index(strings.ToUpper(g.typed)); i >= 0 {
		g.sel = i
		return
	}
	g.setCustom(g.typed)
}

// openGlyphTable shows the table for the selected row of the tasks page;
// enter writes the glyph to its theme.
func (m *Model) openGlyphTable() tea.Cmd {
	r := m.selectedRow()
	if r == nil {
		m.setStatus("no project or task selected")
		return nil
	}
	row := *r
	th := m.rowTheme(row)
	title := "glyph for " + row.name()
	g := newGlyphTable(title, th.Glyph, m.glyphPreview(row, func() store.Theme { return th }, nil))
	g.done = func(glyph string) tea.Cmd {
		th.Glyph = glyph
		return func() tea.Msg {
			var err error
			if row.Project != nil {
				err = m.store.SetProjectTheme(row.Project.ID, th)
			} else {
				err = m.store.SetTaskTheme(row.Task.ID, th)
			}
			if err != nil {
				return actionMsg{err: err}
			}
			return actionMsg{status: row.name() + " is now " + glyph}
		}
	}
	m.glyph, m.mode = g, modeGlyph
	return nil
}

func (m *Model) rowTheme(r trow) store.Theme {
	if r.Project != nil {
		return r.Project.Theme
	}
	return r.Task.Theme
}

// glyphPreview draws a row with a glyph the way the rail's group header
// and the tasks page show it. theme gives the colour, and name, when set,
// the name or title the edit form holds so far.
func (m *Model) glyphPreview(r trow, theme func() store.Theme, name func() string) func(string) []string {
	return func(glyph string) []string {
		th := theme()
		g := themeStyle(th).Render(glyph)
		var header, row string
		if r.Project != nil {
			n := r.Project.Name
			if name != nil {
				n = name()
			}
			header, row = n, g+" "+cTitle.Render(n)
		} else {
			n := r.Task.Title
			if name != nil {
				n = name()
			}
			header = n
			if p := m.store.Project(m.store.ProjectOf(r.Task.ID)); p != nil {
				header = p.Name + " › " + n
			}
			row = "  " + g + "   " + n
		}
		return []string{
			cDim.Render("rail   ") + g + " " + cHeader.Render(header),
			cDim.Render("tasks  ") + row,
		}
	}
}

// glyphField makes field i of f, a glyph, open the table: ctrl+g from it at
// any time, and tab or enter while it is empty. The table opens on the
// typed glyph or the default and puts what is picked in the field.
func (m *Model) glyphField(f *form, i int, r trow, colour, name int) {
	f.Fields[i].Hint = "tab, enter or ctrl+g opens the glyph table; or type one"
	theme := func() store.Theme { return store.Theme{Color: f.Fields[colour].result()} }
	nameFn := func() string { return f.Fields[name].result() }
	prev := f.OnKey
	f.OnKey = func(key string) bool {
		if prev != nil && prev(key) {
			return true
		}
		cur := f.Fields[i]
		if f.Cur != i || !(key == "ctrl+g" || (cur.Value == "" && (key == "tab" || key == "enter"))) {
			return false
		}
		g := newGlyphTable(f.Title+" · glyph", cur.result(), m.glyphPreview(r, theme, nameFn))
		g.back, g.backAt = f, i
		m.glyph, m.mode = g, modeGlyph
		return true
	}
}

func (m *Model) handleGlyphKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	g := m.glyph
	iw := formWidth(m.width) - 4
	close := func() {
		m.glyph = nil
		if g.back != nil {
			m.mode, m.form = modeForm, g.back
			return
		}
		m.mode = modeNormal
	}
	switch msg.String() {
	case "esc", "ctrl+c":
		close()
		if g.back == nil {
			m.setStatus("cancelled")
		}
		return m, nil
	case "enter":
		glyph := g.items[g.sel]
		close()
		if g.back != nil {
			g.back.Fields[g.backAt].Value = glyph
			return m, nil
		}
		return m, g.done(glyph)
	case "h", "left":
		g.sel = max(g.sel-1, 0)
	case "l", "right":
		g.sel = min(g.sel+1, len(g.items)-1)
	case "k", "up":
		g.moveRow(-1, iw)
	case "j", "down":
		g.moveRow(1, iw)
	case "backspace":
		g.typed = ""
		if g.custom != "" {
			g.items, g.labels = g.items[:len(g.items)-1], g.labels[:len(g.labels)-1]
			g.custom, g.sel = "", min(g.sel, len(g.items)-1)
		}
	default:
		k := msg.Key()
		if k.Text != "" && k.Mod&^(tea.ModShift|tea.ModCapsLock|tea.ModNumLock|tea.ModScrollLock) == 0 {
			g.typeText(k.Text)
		}
	}
	return m, nil
}

// glyphBox draws the table as a box width wide and at most maxH tall: the
// preview, the sections with the selection in reverse, and the keys. A
// table taller than the box scrolls to keep the selection in sight.
func (m *Model) glyphBox(width, maxH int) []string {
	g := m.glyph
	iw := width - 4
	cw := g.cellWidth()
	var grid []string
	selLine := 0
	rows, label := g.layout(iw)
	for ri, row := range rows {
		if label[ri] != "" {
			grid = append(grid, cHeader.Render(label[ri]))
			continue
		}
		var b strings.Builder
		for _, it := range row {
			glyph := g.items[it]
			cell := " " + glyph + strings.Repeat(" ", cw-1-ansi.StringWidth(glyph))
			if it == g.sel {
				cell = cSel.Render(cell)
				selLine = len(grid)
			}
			b.WriteString(cell)
		}
		grid = append(grid, b.String())
	}
	top := g.preview(g.items[g.sel])
	top = append(top, cDim.Render(fmt.Sprintf("%s · %s", g.items[g.sel], g.labels[g.sel])))
	room := max(maxH-len(top)-5, 1)
	if len(grid) > room {
		start := min(max(selLine-room/2, 0), len(grid)-room)
		grid = grid[start : start+room]
	}
	lines := []string{boxTop(g.title, width)}
	for _, l := range top {
		lines = append(lines, boxRow(ansi.Truncate(l, iw, "…"), iw))
	}
	lines = append(lines, cBorder.Render("├"+strings.Repeat("─", width-2)+"┤"))
	for _, l := range grid {
		lines = append(lines, boxRow(l, iw))
	}
	lines = append(lines,
		cBorder.Render("├"+strings.Repeat("─", width-2)+"┤"),
		boxRow(cDim.Render(ansi.Truncate(keyHelpHint, iw, "…")), iw),
		cBorder.Render("╰"+strings.Repeat("─", width-2)+"╯"),
	)
	return lines
}
