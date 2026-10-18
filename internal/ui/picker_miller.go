package ui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// millerPicker is prototype 5: a column per level, projects then tasks
// then subtasks, the path of the highlighted node as a breadcrumb above
// them. l and h move between the columns; enter picks the highlighted
// node of the focused column, at any depth.
type millerPicker struct {
	pickBase
	sel     []int // the highlighted entry of each column
	col     int
	preview bool
}

func (c *millerPicker) open(m *Model, p purpose) {
	c.init(m, p)
	c.sel = []int{0}
}

// columns is the first level and, under each highlighted node, the next
// one, as far as the levels go.
func (c *millerPicker) columns() [][]pnode {
	cols := [][]pnode{c.m.pickRoots(c.purpose)}
	for i := 0; ; i++ {
		col := cols[i]
		if len(col) == 0 {
			break
		}
		for len(c.sel) <= i {
			c.sel = append(c.sel, 0)
		}
		if c.sel[i] >= len(col) {
			c.sel[i] = len(col) - 1
		}
		next := c.m.pickChildren(col[c.sel[i]])
		if len(next) == 0 {
			break
		}
		cols = append(cols, next)
	}
	if c.col >= len(cols) {
		c.col = len(cols) - 1
	}
	return cols
}

func (c *millerPicker) current() *pnode {
	cols := c.columns()
	if len(cols[c.col]) == 0 {
		return nil
	}
	return &cols[c.col][c.sel[c.col]]
}

func (c *millerPicker) previewing() *pnode {
	if !c.preview {
		return nil
	}
	return c.current()
}

func (c *millerPicker) handleKey(msg tea.KeyPressMsg) tea.Cmd {
	cols := c.columns()
	n := len(cols[c.col])
	key := msg.String()
	switch key {
	case "esc":
		c.finish(nil)
	case "?":
		c.preview = !c.preview
	case "enter":
		if cur := c.current(); cur != nil {
			pick := *cur
			c.finish(&pick)
		}
	case "j", "down", "ctrl+n":
		if c.sel[c.col]+1 < n {
			c.sel[c.col]++
			c.sel = c.sel[:c.col+1]
		}
	case "k", "up", "ctrl+p":
		if c.sel[c.col] > 0 {
			c.sel[c.col]--
			c.sel = c.sel[:c.col+1]
		}
	case "g", "home":
		c.sel[c.col] = 0
		c.sel = c.sel[:c.col+1]
	case "G", "end":
		c.sel[c.col] = n - 1
		c.sel = c.sel[:c.col+1]
	case "l", "right", "tab":
		if c.col+1 < len(cols) {
			c.col++
		}
	case "h", "left", "shift+tab":
		if c.col > 0 {
			c.col--
		}
	}
	return nil
}

func (c *millerPicker) view(width, height int) []string {
	cols := c.columns()
	iw := width - 4
	bodyH := height - 4
	if bodyH < 4 {
		bodyH = 4
	}
	// The breadcrumb: every highlighted node down to the focused column.
	var crumbs []string
	for i := 0; i <= c.col && i < len(cols); i++ {
		n := cols[i][c.sel[i]]
		name := nodeName(n)
		if i == c.col {
			name = cTitle.Render(name)
		}
		crumbs = append(crumbs, nodeGlyph(n)+" "+name)
	}
	body := []string{" " + strings.Join(crumbs, cDim.Render(" › ")), cBorder.Render(strings.Repeat("─", iw))}
	listH := bodyH - 2
	areaW := iw
	if c.preview {
		areaW = iw/2 - 1
	}
	// Three columns at most: the one before the focus, the focus and the
	// one after.
	first := c.col - 1
	if first < 0 {
		first = 0
	}
	shown := 3
	if c.preview {
		shown = 2
		first = c.col
		if c.col+1 >= len(cols) && c.col > 0 {
			first = c.col - 1
		}
	}
	colW := (areaW - (shown - 1)) / shown
	grid := make([]string, listH)
	for k := 0; k < shown; k++ {
		ci := first + k
		var col []pnode
		if ci < len(cols) {
			col = cols[ci]
		}
		start := 0
		if ci < len(cols) {
			start = window(c.sel[ci], listH, len(col))
		}
		for r := 0; r < listH; r++ {
			cell := ""
			if i := start + r; i < len(col) {
				n := col[i]
				arrow := " "
				if len(c.m.pickChildren(n)) > 0 {
					arrow = "›"
				}
				name := ansi.Truncate(nodeName(n), colW-5, "…")
				cell = " " + nodeGlyph(n) + " " + pad(name, colW-5) + cDim.Render(arrow)
				switch {
				case i == c.sel[ci] && ci == c.col:
					cell = cSel.Render(ansi.Strip(pad(cell, colW)))
				case i == c.sel[ci]:
					cell = cTitle.Render(ansi.Strip(pad(cell, colW)))
				}
			}
			sep := ""
			if k > 0 {
				sep = cBorder.Render("│")
			}
			grid[r] += sep + pad(cell, colW)
		}
	}
	if c.preview {
		pv := c.m.previewLines(c.current(), iw-areaW-2, listH)
		for r := range grid {
			grid[r] = pad(grid[r], areaW) + cBorder.Render(" │") + pv[r]
		}
	}
	body = append(body, grid...)
	return pickerBox(c.m.protoRun.title, body, width)
}
