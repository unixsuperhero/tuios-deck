package ui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/unixsuperhero/tuios-deck/internal/panes"
)

var (
	// The deck runs in any terminal, so the palette is the 16 ANSI colours and
	// follows whatever theme the terminal has.
	cTitle  = lipgloss.NewStyle().Bold(true)
	cDim    = lipgloss.NewStyle().Faint(true)
	cHeader = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("6"))
	cSel    = lipgloss.NewStyle().Reverse(true)
	cStatus = lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
	cErr    = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
	cTabOn  = lipgloss.NewStyle().Bold(true).Reverse(true)
	cTabOff = lipgloss.NewStyle().Faint(true)
	cBorder = lipgloss.NewStyle().Faint(true)
	// An unassigned pane is meant to be hard to miss.
	cUnassigned = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("0")).Background(lipgloss.Color("1"))
	stateFg     = map[string]string{
		"working": "3", "needs_input": "1", "errored": "1", "idle": "2", "done": "4",
	}
	stateDot = map[string]string{
		"working": "◐", "needs_input": "●", "errored": "✗", "idle": "○", "done": "✓",
	}
)

func stateStyle(state string) lipgloss.Style {
	if c, ok := stateFg[state]; ok {
		return lipgloss.NewStyle().Foreground(lipgloss.Color(c))
	}
	return cDim
}

// View draws the deck.
func (m *Model) View() tea.View {
	v := tea.NewView("")
	v.AltScreen = true
	if m.width == 0 {
		v.SetContent("loading…")
		return v
	}
	if m.width < 4 || m.height < 3 {
		v.SetContent(m.focusScreen(pad("resize terminal", m.width)))
		return v
	}
	if m.help != nil {
		v.SetContent(m.focusScreen(m.helpView()))
		return v
	}
	if m.focus != nil && !m.focus.clock.BufferUntil.IsZero() {
		v.SetContent(m.focusBufferView())
		return v
	}
	left := min(m.leftWidth(), m.width-2)
	if m.page == pageProtos {
		left = m.width / 2
	}
	right := m.width - left - 1
	bodyH := m.height - 3

	var b strings.Builder
	b.WriteString(m.titleLine())
	b.WriteByte('\n')

	leftLines, rightLines := m.listLines(left, bodyH), m.detailLines(right, bodyH)
	switch m.page {
	case pageReviews:
		leftLines, rightLines = m.reviewListLines(left, bodyH), m.reviewDetailLines(right, bodyH)
	case pageTasks:
		leftLines, rightLines = m.taskListLines(left, bodyH), m.taskDetailLines(right, bodyH)
	case pageCaptures:
		leftLines, rightLines = m.captureListLines(left, bodyH), m.captureDetailLines(right, bodyH)
	case pageContext:
		leftLines, rightLines = m.contextListLines(left, bodyH), m.contextDetailLines(right, bodyH)
	case pageProtos:
		leftLines, rightLines = m.protoListLines(left, bodyH), m.protoDetailLines(right, bodyH)
	case pageHistory:
		leftLines, rightLines = m.historyListLines(left, bodyH), m.historyDetailLines(right, bodyH)
	}
	// The palette lies over the lower part of the body, full width.
	var under []string
	if m.mode == modePalette && m.pal != nil {
		under = m.paletteLines(m.width, bodyH)
	}
	// A form is a box centred over the body.
	var box []string
	boxW, boxX, boxY := 0, 0, 0
	if m.mode == modeForm && m.form != nil {
		boxW = formWidth(m.width)
		box = m.formBox(boxW, bodyH)
		boxX, boxY = (m.width-boxW)/2, (bodyH-len(box))/2
	}
	if m.mode == modeProto && m.proto != nil {
		boxW = protoWidth(m.width)
		box = m.proto.view(boxW, bodyH)
		boxX, boxY = (m.width-boxW)/2, (bodyH-len(box))/2
	}
	if m.mode == modePrompts && m.pm != nil {
		boxW = formWidth(m.width)
		box = m.modalBox(boxW, bodyH)
		boxX, boxY = (m.width-boxW)/2, (bodyH-len(box))/2
	}
	if m.mode == modeGlyph && m.glyph != nil {
		boxW = formWidth(m.width)
		box = m.glyphBox(boxW, bodyH)
		boxX, boxY = (m.width-boxW)/2, (bodyH-len(box))/2
	}
	for i := 0; i < bodyH; i++ {
		if j := i - (bodyH - len(under)); j >= 0 {
			b.WriteString(pad(under[j], m.width))
			b.WriteByte('\n')
			continue
		}
		line := pad(leftLines[i], left) + cBorder.Render("│") + pad(rightLines[i], right)
		if j := i - boxY; j >= 0 && j < len(box) {
			line = overlay(line, box[j], boxX, boxW)
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	b.WriteString(pad(m.statusLine(), m.width))
	b.WriteByte('\n')
	b.WriteString(pad(m.helpLine(), m.width))
	v.SetContent(m.focusScreen(b.String()))
	return v
}

func (m *Model) titleLine() string {
	if m.focus != nil {
		name, _ := m.focusTarget()
		s := " FOCUS · " + name
		if m.page == pageDeck {
			s += " · sort: " + m.railSort.String()
		}
		return pad(s, m.width)
	}
	agents, attention := 0, 0
	for _, r := range m.rows {
		if r.Pane != nil && r.Pane.IsAgent {
			agents++
			if r.Pane.NeedsYou {
				attention++
			}
		}
	}
	s := cTitle.Render(" tuios-deck ") + cTabOn.Render(" "+m.page.String()+" ")
	if m.page == pageTasks && m.easyOnly {
		s += " · focus plan: saved easy tasks"
	}
	if m.page == pageDeck && !m.grouping.bySession() {
		s += " by " + m.grouping.String()
	}
	if m.page == pageDeck {
		s += " · sort: " + m.railSort.String()
	}
	if m.page == pageContext {
		s += " " + m.ctxTitle()
	}
	s += " " + cDim.Render(m.daemonInfo+"  "+m.client.Socket)
	right := fmt.Sprintf("%d agents", agents)
	if attention > 0 {
		right = cErr.Render(fmt.Sprintf("%d need you", attention)) + "  " + right
	}
	if m.eventsErr != "" {
		right = cErr.Render(m.eventsErr) + "  " + right
	}
	gap := m.width - ansi.StringWidth(s) - ansi.StringWidth(right) - 1
	if gap < 1 {
		gap = 1
	}
	return s + strings.Repeat(" ", gap) + right
}

func (m *Model) listLines(width, height int) []string {
	lines := make([]string, 0, height)
	if m.listErr != "" {
		lines = append(lines, cErr.Render(ansi.Truncate(m.listErr, width, "…")))
	}
	if len(m.rows) == 0 && m.listErr == "" {
		lines = append(lines, cDim.Render("no sessions: press n or t"))
	}
	// Keep the selection on screen.
	start := 0
	if m.sel >= height {
		start = m.sel - height + 1
	}
	for i := start; i < len(m.rows) && len(lines) < height; i++ {
		r := m.rows[i]
		var line string
		if r.Pane == nil {
			tag := ""
			if r.Session != nil && r.Session.Attached {
				tag = " ·attached"
			}
			indent := strings.Repeat("  ", r.Depth)
			if r.Theme != nil {
				line = indent + themeStyle(*r.Theme).Render(r.Theme.Glyph) + " " + cHeader.Render(ansi.Truncate(r.Header, width-len(indent)-2, "…"))
			} else {
				line = indent + cHeader.Render(ansi.Truncate("▾ "+r.Header, width-len(indent)-len(tag), "…")) + cDim.Render(tag)
			}
		} else {
			p := r.Pane
			dot := "·"
			if p.IsAgent {
				dot = stateDot[p.State]
				if dot == "" {
					dot = "?"
				}
			}
			state := p.State
			if !p.IsAgent {
				state = p.Fg
			}
			if p.Queued > 0 {
				state += fmt.Sprintf(" +%d", p.Queued)
			}
			line = m.paneRow(p, dot, state, r.Depth, width, i == m.sel && !m.focusMain)
			if i == m.sel && m.focusMain {
				// The main area has focus: a dim marker keeps the place.
				line = cDim.Render("▸") + ansi.Cut(line, 1, width)
			}
		}
		lines = append(lines, line)
	}
	for len(lines) < height {
		lines = append(lines, "")
	}
	return lines
}

// Every pane row opens with its kind: AI for an agent, $ for a terminal.
// Both are two columns wide so the rows stay lined up.
var (
	kindAgent    = "AI"
	kindTerminal = "$ "
	cKindAgent   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("6"))
)

// paneRow is a pane's line in the rail, indented by its depth: its kind
// tag, its state dot, the project's first four letters and the task title in
// their theme colours, the pane name, and the state flush right, followed by
// an agent's harness, dimmed, when the rail has room for it. A narrow rail
// drops the harness first, then truncates the task title, then the name. An
// unassigned pane shows !!!! and its name in black on red. The selected row
// is drawn in reverse over each part's own colours, so the theme colours
// stay visible.
func (m *Model) paneRow(p *panes.Pane, dot, state string, depth, width int, sel bool) string {
	plain := lipgloss.NewStyle()
	chip, chipStyle, task, taskStyle, nameStyle := "!!!!", cUnassigned, "", plain, cUnassigned
	if a, ok := m.assignmentOf(p.ID); ok {
		chip, chipStyle, task, taskStyle, nameStyle = a.chip, a.chipStyle, a.task, a.taskStyle, plain
	}
	kind, kindStyle := kindTerminal, cDim
	if p.IsAgent {
		kind, kindStyle = kindAgent, cKindAgent
	}
	indent := strings.Repeat("  ", max(depth, 1))
	lead := len(indent) + 10 // the indent, the kind, " ", the dot, " ", the chip, " "
	// fit lays out the task title and the name before a right-hand part
	// tail columns wide, and says whether both kept all their text.
	fit := func(tail int) (string, string, bool) {
		room := width - lead - tail - 1
		name := ansi.Truncate(p.Name, room, "…")
		t := ""
		if left := room - ansi.StringWidth(name) - 2; task != "" && left >= 4 {
			t = ansi.Truncate(task, left, "…") + "  "
		}
		whole := name == p.Name && (task == "" || t == task+"  ")
		return t, name, whole
	}
	harness := ""
	if p.IsAgent && p.Harness != "" {
		harness = " " + p.Harness
	}
	shownTask, name, whole := fit(ansi.StringWidth(state + harness))
	if harness != "" && !whole {
		harness = ""
		shownTask, name, _ = fit(ansi.StringWidth(state))
	}
	fill := width - lead - ansi.StringWidth(shownTask) - ansi.StringWidth(name) - ansi.StringWidth(state+harness)
	if fill < 1 {
		fill = 1
	}
	r := func(s lipgloss.Style, text string) string {
		if sel {
			s = s.Reverse(true)
		}
		return s.Render(text)
	}
	return r(plain, indent) + r(kindStyle, kind) + r(plain, " ") + r(stateStyle(p.State), dot) + r(plain, " ") +
		r(chipStyle, chip) + r(plain, " ") + r(taskStyle, shownTask) + r(nameStyle, name) +
		r(plain, strings.Repeat(" ", fill)) + r(stateStyle(p.State), state) + r(cDim, harness)
}

func (m *Model) detailLines(width, height int) []string {
	if m.result != nil {
		return m.resultLines(width, height)
	}
	lines := make([]string, 0, height)
	var tabs []string
	// With the main area focused the bar brightens and the shown tab is
	// underlined, so the person sees where j and k go.
	on, off := cTabOn, cTabOff
	if m.focusMain {
		on, off = cTabOn.Underline(true), lipgloss.NewStyle()
	}
	for t := tab(0); t < numTabs; t++ {
		label := fmt.Sprintf(" %d %s ", int(t)+1, t.String())
		if t == m.tab {
			tabs = append(tabs, on.Render(label))
		} else {
			tabs = append(tabs, off.Render(label))
		}
	}
	bar := strings.Join(tabs, " ")
	if p := m.selected(); p != nil {
		info := p.Session + " › " + p.Name
		if a, ok := m.assignmentOf(p.ID); ok {
			info += "  " + a.style.Render(a.glyph) + " " + a.path
		}
		if p.IsAgent && p.Message != "" {
			info += "  " + cDim.Render(panes.OneLine(p.Message))
		}
		bar += "  " + ansi.Truncate(info, width-ansi.StringWidth(bar)-2, "…")
	}
	lines = append(lines, bar)
	body := height - 1
	if m.selected() == nil {
		lines = append(lines, cDim.Render("select a pane on the left"))
	} else if m.detailErr != "" {
		lines = append(lines, cErr.Render(ansi.Truncate(m.detailErr, width, "…")))
	} else {
		end := m.scroll + body
		if end > len(m.detail) {
			end = len(m.detail)
		}
		for _, l := range m.detail[m.scroll:end] {
			lines = append(lines, ansi.Truncate(l, width, ""))
		}
	}
	for len(lines) < height {
		lines = append(lines, "")
	}
	if !m.follow && len(m.detail) > body {
		lines[height-1] = pad(lines[height-1], width-12) + cDim.Render(fmt.Sprintf("%4d/%d ↓G", m.scroll+1, len(m.detail)))
	}
	return lines
}

func (m *Model) statusLine() string {
	switch m.mode {
	case modeForm:
		line := cStatus.Render(m.form.Title + " · " + m.form.Fields[m.form.Cur].Label)
		if m.form.Err != "" {
			line += "  " + cErr.Render(m.form.Err)
		}
		return line
	case modeConfirm:
		return cStatus.Render(m.confirm.Text)
	case modeProto:
		return cStatus.Render(m.protoRun.title)
	case modeGlyph:
		return cStatus.Render(m.glyph.title)
	}
	if strings.HasPrefix(m.status, "error:") {
		return cErr.Render(m.status)
	}
	return cStatus.Render(m.status)
}

func (m *Model) helpLine() string {
	return cDim.Render(" " + keyHelpHint)
}

// pad fits s to width: truncated when longer, padded with spaces when shorter.
func pad(s string, width int) string {
	if width <= 0 {
		return ""
	}
	w := ansi.StringWidth(s)
	if w > width {
		return ansi.Truncate(s, width, "")
	}
	return s + strings.Repeat(" ", width-w)
}
