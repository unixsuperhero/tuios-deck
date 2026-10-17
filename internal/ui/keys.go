package ui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/unixsuperhero/tuios-deck/internal/panes"
)

func (m *Model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch m.mode {
	case modeForm:
		switch msg.String() {
		case "esc", "ctrl+c":
			m.mode, m.form = modeNormal, nil
			m.setStatus("cancelled")
			return m, nil
		}
		done, cmd := m.form.handleKey(msg)
		if done {
			m.mode, m.form = modeNormal, nil
		}
		return m, cmd

	case modeConfirm:
		switch msg.String() {
		case "y", "Y":
			m.mode = modeNormal
			return m, m.confirm.Do
		default:
			m.mode = modeNormal
			m.setStatus("cancelled")
			return m, nil
		}

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
	}
	if m.page == pageTasks {
		return m.handleTasksKey(msg)
	}
	if m.page == pageCaptures {
		return m.handleCapturesKey(msg)
	}

	switch msg.String() {
	case "j", "down", "k", "up", "g", "home", "tab", "l", "right", "shift+tab", "h", "left", "1", "2", "3", "r", "enter":
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
			m.detail, m.revision = nil, 0
		}
		return m, m.loadDetail(true)
	case "tab", "l", "right":
		m.tab = (m.tab + 1) % 3
		m.detail, m.detailErr, m.revision = nil, "", 0
		m.follow = true
		return m, m.loadDetail(true)
	case "shift+tab", "h", "left":
		m.tab = (m.tab + 2) % 3
		m.detail, m.detailErr, m.revision = nil, "", 0
		m.follow = true
		return m, m.loadDetail(true)
	case "1", "2", "3":
		m.tab = tab(msg.String()[0] - '1')
		m.detail, m.detailErr, m.revision = nil, "", 0
		m.follow = true
		return m, m.loadDetail(true)
	case "J", "ctrl+d", "pgdown":
		m.follow = false
		m.scroll += m.detailHeight() / 2
		m.clampScroll()
		return m, nil
	case "K", "ctrl+u", "pgup":
		m.follow = false
		m.scroll -= m.detailHeight() / 2
		m.clampScroll()
		return m, nil
	case "G", "end":
		m.follow = true
		m.clampScroll()
		return m, nil
	case "r", "enter":
		m.detail, m.revision = nil, 0
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
	case "x":
		p := m.selected()
		if p == nil {
			return m, nil
		}
		return m, m.confirmClose(p)
	}
	return m, nil
}

// confirmClose asks before closing the pane.
func (m *Model) confirmClose(p *panes.Pane) tea.Cmd {
	m.mode = modeConfirm
	m.confirm.Text = "close " + p.Name + " in " + p.Session + "? (y/N)"
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
	fields = append(fields, field{Label: "first prompt", Keep: true})
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
		Fields: []field{{Label: label, Keep: true}},
		Done: func(v []string) tea.Cmd {
			if v[0] == "" {
				return nil
			}
			return m.sendPrompt(p.Session, p.ID, p.Name, p.IsAgent, v[0])
		},
	}
	return nil
}
