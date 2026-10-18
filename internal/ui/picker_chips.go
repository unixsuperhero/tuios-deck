package ui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// chipsPicker is prototype 6: one input where each segment, once a slash
// ends it, locks as a chip: the project, then its task, then a subtask.
// The segment being typed completes among the children of the last chip.
type chipsPicker struct {
	pickBase
	chips   []pnode
	text    string
	hl      int
	err     string
	preview bool
}

func (c *chipsPicker) open(m *Model, p purpose) { c.init(m, p) }

// level is the nodes the segment being typed chooses among.
func (c *chipsPicker) level() []pnode {
	if len(c.chips) == 0 {
		return c.m.pickRoots(c.purpose)
	}
	return c.m.pickChildren(c.chips[len(c.chips)-1])
}

// completions is the level ranked against the segment, an exact name
// first.
func (c *chipsPicker) completions() []scored {
	list := c.m.fuzzyNodes(c.level(), c.text, nodeName)
	for i, s := range list {
		if strings.EqualFold(nodeName(s.n), strings.TrimSpace(c.text)) && i > 0 {
			list = append([]scored{s}, append(list[:i:i], list[i+1:]...)...)
			break
		}
	}
	return list
}

// chosen is the completion the highlight is on, or nil.
func (c *chipsPicker) chosen() *pnode {
	list := c.completions()
	if c.hl < len(list) {
		return &list[c.hl].n
	}
	return nil
}

// spelled is the node the chips and the segment spell: the chosen
// completion when there is text, else the last chip.
func (c *chipsPicker) spelled() *pnode {
	if strings.TrimSpace(c.text) != "" {
		return c.chosen()
	}
	if len(c.chips) > 0 {
		n := c.chips[len(c.chips)-1]
		return &n
	}
	return nil
}

func (c *chipsPicker) previewing() *pnode {
	if !c.preview {
		return nil
	}
	if n := c.spelled(); n != nil {
		return n
	}
	return c.chosen()
}

// lock turns the chosen completion into a chip.
func (c *chipsPicker) lock() {
	n := c.chosen()
	if n == nil {
		c.err = "nothing matches " + strings.TrimSpace(c.text)
		return
	}
	c.chips = append(c.chips, *n)
	c.text, c.hl = "", 0
}

func (c *chipsPicker) handleKey(msg tea.KeyPressMsg) tea.Cmd {
	c.err = ""
	key := msg.String()
	list := c.completions()
	switch {
	case key == "esc":
		c.finish(nil)
	case key == "enter":
		n := c.spelled()
		switch {
		case n != nil:
			pick := *n
			c.finish(&pick)
		case strings.TrimSpace(c.text) != "" && c.purpose == purposeFilter:
			// A segment that names nothing is a text to match.
			pick := pnode{Kind: "any"}
			if len(c.chips) > 0 {
				pick = c.chips[len(c.chips)-1]
			}
			pick.Text = strings.TrimSpace(c.text)
			c.finish(&pick)
		case strings.TrimSpace(c.text) != "":
			c.err = "nothing matches " + strings.TrimSpace(c.text)
		default:
			c.err = "type a project"
			if c.purpose == purposeFilter {
				c.err = "type a kind, or any"
			}
		}
	case key == "tab":
		c.lock()
	case key == "/":
		if strings.TrimSpace(c.text) == "" {
			return nil
		}
		c.lock()
	case key == "backspace" && c.text == "":
		if len(c.chips) > 0 {
			c.chips = c.chips[:len(c.chips)-1]
			c.hl = 0
		}
	case moveSel(&c.hl, len(list), key):
	case editText(&c.text, msg):
		c.hl = 0
	}
	return nil
}

func (c *chipsPicker) view(width, height int) []string {
	iw := width - 4
	var b strings.Builder
	b.WriteString(" ")
	for _, ch := range c.chips {
		style := cSel
		switch {
		case ch.Task != nil:
			style = themeStyle(ch.Task.Theme).Reverse(true)
		case ch.Project != nil:
			style = themeStyle(ch.Project.Theme).Reverse(true)
		case ch.Kind != "":
			style = kindStyle(ch.Kind).Reverse(true)
		}
		b.WriteString(style.Render(" "+nodeName(ch)+" ") + cDim.Render(" / "))
	}
	b.WriteString(c.text + "▏")
	input := tail(b.String(), iw)
	body := []string{input}
	if c.err != "" {
		body = append(body, " "+cErr.Render(c.err))
	} else if n := c.spelled(); n != nil {
		body = append(body, " "+cDim.Render("enter picks ")+c.m.nodePath(*n))
	} else {
		body = append(body, "")
	}
	body = append(body, cBorder.Render(strings.Repeat("─", iw)))
	list := c.completions()
	rows := listRows
	listW := iw
	if c.preview {
		listW = iw/2 - 1
		rows = height - 4 - len(body)
		if rows > 18 {
			rows = 18
		}
		if rows < listRows {
			rows = listRows
		}
	}
	var lines []string
	start := window(c.hl, rows, len(list))
	for i := start; i < len(list) && len(lines) < rows; i++ {
		s := list[i]
		s.path = nodeName(s.n)
		if len(c.m.pickChildren(s.n)) > 0 {
			s.path += "/"
		}
		lines = append(lines, nodeRow(s, listW, i == c.hl))
	}
	if len(list) == 0 {
		msg := "   nothing matches"
		if c.purpose == purposeFilter {
			msg += "; enter filters by the text"
		}
		lines = append(lines, cDim.Render(msg))
	}
	for len(lines) < rows {
		lines = append(lines, "")
	}
	if c.preview {
		pv := c.m.previewLines(c.previewing(), iw-listW-2, rows)
		for i := range lines {
			lines[i] = pad(lines[i], listW) + cBorder.Render(" │") + pv[i]
		}
	}
	body = append(body, lines...)
	for i := range body {
		body[i] = ansi.Truncate(body[i], iw, "")
	}
	help := "/ or tab lock a chip · backspace pops · ctrl+n/ctrl+p move · enter pick · esc cancel"
	return pickerBox(c.m.protoRun.title, body, help, width)
}
