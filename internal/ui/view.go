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
	stateFg = map[string]string{
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
	left := m.leftWidth()
	right := m.width - left - 1
	bodyH := m.height - 3

	var b strings.Builder
	b.WriteString(m.titleLine())
	b.WriteByte('\n')

	leftLines, rightLines := m.listLines(left, bodyH), m.detailLines(right, bodyH)
	switch m.page {
	case pageTasks:
		leftLines, rightLines = m.taskListLines(left, bodyH), m.taskDetailLines(right, bodyH)
	case pageCaptures:
		leftLines, rightLines = m.captureListLines(left, bodyH), m.captureDetailLines(right, bodyH)
	}
	for i := 0; i < bodyH; i++ {
		b.WriteString(pad(leftLines[i], left))
		b.WriteString(cBorder.Render("│"))
		b.WriteString(pad(rightLines[i], right))
		b.WriteByte('\n')
	}
	b.WriteString(pad(m.statusLine(), m.width))
	b.WriteByte('\n')
	b.WriteString(pad(m.helpLine(), m.width))
	v.SetContent(b.String())
	return v
}

func (m *Model) titleLine() string {
	agents, attention := 0, 0
	for _, r := range m.rows {
		if r.Pane != nil && r.Pane.IsAgent {
			agents++
			if r.Pane.NeedsYou {
				attention++
			}
		}
	}
	s := cTitle.Render(" tuios-deck ") + cTabOn.Render(" "+m.page.String()+" ") + " " + cDim.Render(m.daemonInfo+"  "+m.client.Socket)
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
			line = cHeader.Render(ansi.Truncate("▾ "+r.Header, width-len(tag), "…")) + cDim.Render(tag)
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
			// The assignment's glyph sits before the name in its theme
			// colour; the task title follows the name when it has room.
			glyph, title := " ", ""
			if a, ok := m.assignmentOf(p.ID); ok {
				glyph, title = a.style.Render(a.glyph), a.short
			}
			name := ansi.Truncate(p.Name, width-6-ansi.StringWidth(state)-1, "…")
			room := width - 6 - ansi.StringWidth(name) - ansi.StringWidth(state) - 2
			if title != "" && room >= 4 {
				title = " " + cDim.Render(ansi.Truncate(title, room-1, "…"))
			} else {
				title = ""
			}
			fill := width - 6 - ansi.StringWidth(name) - ansi.StringWidth(title) - ansi.StringWidth(state)
			if fill < 1 {
				fill = 1
			}
			line = "  " + stateStyle(p.State).Render(dot) + " " + glyph + " " + name + title + strings.Repeat(" ", fill) + stateStyle(p.State).Render(state)
			if i == m.sel {
				line = cSel.Render(ansi.Strip(pad(line, width)))
			}
		}
		lines = append(lines, line)
	}
	for len(lines) < height {
		lines = append(lines, "")
	}
	return lines
}

func (m *Model) detailLines(width, height int) []string {
	lines := make([]string, 0, height)
	var tabs []string
	for t := tabScreen; t <= tabTranscript; t++ {
		label := fmt.Sprintf(" %d %s ", int(t)+1, t.String())
		if t == m.tab {
			tabs = append(tabs, cTabOn.Render(label))
		} else {
			tabs = append(tabs, cTabOff.Render(label))
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
		f := m.form.Fields[m.form.Cur]
		prompt := cStatus.Render(m.form.Title+" · "+f.Label) + ": "
		value := f.Value
		if value == "" && f.Default != "" {
			value = cDim.Render(f.Default + " (tab to take)")
		}
		line := prompt + value + "▏"
		if m.form.Err != "" {
			line += "  " + cErr.Render(m.form.Err)
		}
		return line
	case modeConfirm:
		return cStatus.Render(m.confirm.Text)
	}
	if strings.HasPrefix(m.status, "error:") {
		return cErr.Render(m.status)
	}
	return cStatus.Render(m.status)
}

func (m *Model) helpLine() string {
	switch m.mode {
	case modeForm:
		f := m.form.Fields[m.form.Cur]
		help := fmt.Sprintf(" field %d/%d · enter next · tab default · shift+tab back · esc cancel", m.form.Cur+1, len(m.form.Fields))
		if f.Options != nil {
			help = fmt.Sprintf(" field %d/%d · enter next · tab complete · shift+tab back · esc cancel", m.form.Cur+1, len(m.form.Fields))
			if c := f.completions(); len(c) > 0 {
				const show = 4
				more := ""
				if len(c) > show {
					more, c = fmt.Sprintf(" +%d more", len(c)-show), c[:show]
				}
				help += "  ⇥ " + strings.Join(c, " · ") + more
			}
		}
		return cDim.Render(help)
	case modeConfirm:
		return cDim.Render(" y confirm · any other key cancels")
	}
	switch m.page {
	case pageTasks:
		return cDim.Render(" j/k select · N project · n task/subtask · e rename · s done · m colour · M glyph · a assign pane · enter go to pane · x delete · esc deck · q quit")
	case pageCaptures:
		return cDim.Render(" j/k select · J/K scroll · G follow · enter go to pane · x delete · r refresh · T tasks · esc deck · q quit")
	}
	return cDim.Render(" j/k select · 1/2/3 tab · J/K scroll · G follow · n agent · t terminal · p prompt · a assign · c capture · x close · T tasks · C captures · r refresh · q quit")
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
