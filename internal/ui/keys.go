package ui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/unixsuperhero/tuios-deck/internal/panes"
)

func (m *Model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch m.mode {
	case modeForm:
		if m.form.cwd == "" {
			m.form.cwd = cwdDefault(m.selected())
		}
		if msg.String() == "esc" && m.form.escape() {
			return m, nil
		}
		switch msg.String() {
		case "esc", "ctrl+c":
			if m.formStack != nil {
				m.form, m.formStack = m.formStack, nil
				m.form.adding = 0
				return m, nil
			}
			m.mode, m.form = modeNormal, nil
			m.setStatus("cancelled")
			return m, nil
		}
		done, cmd := m.form.handleKey(msg)
		switch {
		case done && m.formStack != nil:
			m.form, m.formStack = m.formStack, nil
		case done:
			m.mode, m.form = modeNormal, nil
		case m.form.adding > 0:
			m.formStack = m.form
			return m, m.openMassAdd(m.addPrefill(m.form))
		}
		return m, cmd

	case modeConfirm:
		switch msg.String() {
		case "enter", "y", "Y":
			m.mode = modeNormal
			return m, m.confirm.Do
		case "n", "N", "esc", "ctrl+c":
			m.mode = modeNormal
			m.setStatus("cancelled")
		}
		// Any other key does nothing, so a stray key cannot confirm or cancel.
		return m, nil

	case modePalette:
		return m.handlePaletteKey(msg)
	}

	// Keys every page answers.
	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit
	case ":", "ctrl+p":
		return m, m.openPalette()
	case "T":
		m.page = pageTasks
		m.rebuildTasks()
		return m, nil
	case "C":
		m.openCaptures()
		return m, nil
	case "A":
		return m, m.openMassAdd("")
	case "I":
		m.openContext()
		return m, nil
	}
	if m.page == pageTasks {
		return m.handleTasksKey(msg)
	}
	if m.page == pageCaptures {
		return m.handleCapturesKey(msg)
	}
	if m.page == pageContext {
		return m.handleContextKey(msg)
	}

	switch msg.String() {
	case "h", "left", "l", "right":
		m.focusMain = msg.String() == "l" || msg.String() == "right"
		if m.tab == tabTurns && m.result == nil && m.detail != nil {
			m.renderTurns() // the focused row's highlight follows the focus
		}
		return m, nil
	}
	if m.focusMain {
		if handled, cmd := m.handleMainKey(msg); handled {
			return m, cmd
		}
	}
	switch msg.String() {
	case "j", "down", "k", "up", "g", "home", "tab", "]", "shift+tab", "[", "1", "2", "3", "4", "r", "enter":
		m.clearResult() // moving the selection or the tab ends a verb's result view
	}
	switch msg.String() {
	case "j", "down":
		m.move(1)
		return m, m.loadDetail(true)
	case "k", "up":
		m.move(-1)
		return m, m.loadDetail(true)
	case "g", "home":
		if i := m.firstPane(0, 1); i >= 0 {
			m.sel = i
			m.detail, m.revision, m.scroll = nil, 0, 0
		}
		return m, m.loadDetail(true)
	case "tab", "]":
		return m, m.setTab((m.tab + 1) % numTabs)
	case "shift+tab", "[":
		return m, m.setTab((m.tab + numTabs - 1) % numTabs)
	case "1", "2", "3", "4":
		return m, m.setTab(tab(msg.String()[0] - '1'))
	case "J", "ctrl+d", "pgdown":
		m.scrollBy(m.detailHeight() / 2)
		return m, nil
	case "K", "ctrl+u", "pgup":
		m.scrollBy(-m.detailHeight() / 2)
		return m, nil
	case "G", "end":
		m.follow = true
		m.clampScroll()
		return m, nil
	case "r", "enter":
		// Keep the lines and the scroll until the fresh read replaces them;
		// revision 0 makes sure it does.
		m.revision = 0
		return m, tea.Batch(m.refreshList(), m.loadDetail(true))
	case "o":
		m.cycleGrouping(1)
		return m, nil
	case "O":
		m.cycleGrouping(-1)
		return m, nil
	case "n":
		return m, m.openAgentForm()
	case "t":
		return m, m.openTerminalForm()
	case "p":
		return m, m.openPromptForm()
	case "a":
		return m, m.openAssignForm()
	case "c":
		return m, m.captureToFile()
	case "i":
		return m, m.extractContext()
	case "x":
		p := m.selected()
		if p == nil {
			return m, nil
		}
		return m, m.confirmClose(p)
	}
	return m, nil
}

// handleMainKey answers the keys that mean something else while the main
// area has focus: j/k and the rest scroll the detail, and the rail's
// selection stays where it is. It reports whether it took the key.
func (m *Model) handleMainKey(msg tea.KeyPressMsg) (bool, tea.Cmd) {
	if m.tab == tabTurns && m.result == nil {
		if handled, cmd := m.handleTurnsKey(msg); handled {
			return true, cmd
		}
	}
	switch msg.String() {
	case "j", "down":
		m.scrollBy(1)
	case "k", "up":
		m.scrollBy(-1)
	case "g", "home":
		m.follow = false
		m.scroll = 0
		m.clampScroll()
	case "enter":
		// Only the Turns tab has rows to act on; it answered above.
	default:
		return false, nil
	}
	return true, nil
}

// scrollBy moves the detail by n lines and stops following its end.
func (m *Model) scrollBy(n int) {
	m.follow = false
	m.scroll += n
	m.clampScroll()
}

// setTab shows another tab of the selected pane from its end.
func (m *Model) setTab(t tab) tea.Cmd {
	m.tab = t
	m.detail, m.detailErr, m.revision = nil, "", 0
	m.scroll, m.follow = 0, true
	return m.loadDetail(true)
}

// confirmClose asks before closing the pane.
func (m *Model) confirmClose(p *panes.Pane) tea.Cmd {
	m.mode = modeConfirm
	m.confirm.Text = "close " + p.Name + " in " + p.Session + "? (Y/n)"
	m.confirm.Do = m.closeWindow(p.Session, p.ID, p.Name)
	return nil
}

func (m *Model) openAgentForm() tea.Cmd {
	p := m.selected()
	session := m.selectedSession()
	if session == "" {
		session = "deck"
	}
	fields := []field{
		{Label: "session", Default: session},
		{Label: "agent", Default: "claude"},
		{Label: "name", Keep: true},
		{Label: "cwd", Default: cwdDefault(p)},
	}
	fields = append(fields, m.targetFields(false)...)
	fields = append(fields, field{Label: "first prompt", Keep: true, Multi: true})
	m.mode = modeForm
	m.form = &form{
		Title:  "new agent",
		Fields: fields,
		Validate: func(v []string) (int, string) {
			_, _, f, msg := m.resolveTarget(v[4], v[5], 4, 5, false)
			return f, msg
		},
		Done: func(v []string) tea.Cmd {
			taskID, projectID, _, _ := m.resolveTarget(v[4], v[5], 4, 5, false)
			return m.startAgent(v[0], v[1], v[2], v[3], v[6], taskID, projectID)
		},
	}
	return nil
}

func (m *Model) openTerminalForm() tea.Cmd {
	p := m.selected()
	session := m.selectedSession()
	if session == "" {
		session = "deck"
	}
	fields := []field{
		{Label: "session", Default: session},
		{Label: "name", Keep: true},
		{Label: "cwd", Default: cwdDefault(p)},
	}
	fields = append(fields, m.targetFields(false)...)
	m.mode = modeForm
	m.form = &form{
		Title:  "new terminal",
		Fields: fields,
		Validate: func(v []string) (int, string) {
			_, _, f, msg := m.resolveTarget(v[3], v[4], 3, 4, false)
			return f, msg
		},
		Done: func(v []string) tea.Cmd {
			taskID, projectID, _, _ := m.resolveTarget(v[3], v[4], 3, 4, false)
			return m.newTerminal(v[0], v[1], v[2], taskID, projectID)
		},
	}
	return nil
}

func (m *Model) openPromptForm() tea.Cmd {
	p := m.selected()
	if p == nil {
		m.setStatus("select a pane first")
		return nil
	}
	if p.BlockedBy != "" {
		m.setStatus(p.Name + " is waiting on " + p.BlockedBy + "; answer it in tuios, not here")
		return nil
	}
	label := "text for " + p.Name
	if p.IsAgent {
		label = "prompt for " + p.Name + " (queued until it is idle)"
	}
	m.mode = modeForm
	m.form = &form{
		Title:  "prompt",
		Fields: []field{{Label: label, Keep: true, Multi: true}},
		Done: func(v []string) tea.Cmd {
			if v[0] == "" {
				return nil
			}
			return m.sendPrompt(p.Session, p.ID, p.Name, p.IsAgent, v[0])
		},
	}
	return nil
}
