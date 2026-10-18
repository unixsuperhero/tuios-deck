package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/unixsuperhero/tuios-deck/internal/config"
	"github.com/unixsuperhero/tuios-deck/internal/panes"
)

func (m *Model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if m.help != nil {
		return m.handleHelpKey(msg)
	}
	if msg.String() == "f1" {
		return m, m.openHelp()
	}
	if handled, cmd := m.focusBufferKey(msg.String()); handled {
		return m, cmd
	}
	if m.live != nil {
		return m.handleLiveKey(msg)
	}
	switch m.mode {
	case modeForm:
		if !m.saveFormPrompts(m.form) {
			return m, nil
		}
		if m.form.recovery != nil {
			m.promptRecoveryKey(msg)
			return m, nil
		}
		m.form.remember()
		if msg.String() == "ctrl+r" && m.form.Fields[m.form.Cur].Prompt {
			m.openPromptRecovery()
			return m, nil
		}
		helpKey := (&config.Config{}).Binding(config.ModeDeck, "help")
		if m.settings != nil {
			helpKey = m.settings.Binding(config.ModeDeck, "help")
		}
		if msg.String() == helpKey && (m.vimModes() && m.form.vimNormal || len([]rune(helpKey)) > 1 && helpKey != "space") {
			return m, m.openHelp()
		}
		if m.form.cwd == "" {
			m.form.cwd = cwdDefault(m.selected())
		}
		if m.vimModes() {
			var handled bool
			if msg, handled = m.vimFormKey(msg); handled {
				return m, nil
			}
		}
		if msg.String() == "esc" && m.form.escape() {
			return m, nil
		}
		switch msg.String() {
		case "esc", "ctrl+c":
			if m.form.changed() {
				m.confirm = confirmState{Text: "close this form without submitting? (Y/n)", Then: m.cancelForm, Back: modeForm}
				m.mode = modeConfirm
				return m, nil
			}
			m.cancelForm()
			return m, nil
		}
		if msg.String() == "ctrl+o" && m.form.Fields[m.form.Cur].Attach {
			return m, m.pickAttachments()
		}
		cur := &m.form.Fields[m.form.Cur]
		if msg.String() == "ctrl+u" && cur.Prompt {
			if !m.stashCurrentPrompt() {
				return m, nil
			}
			if strings.TrimSpace(cur.Value) != "" {
				m.setStatus("prompt stashed; ctrl+r recovers")
			}
		}
		done, cmd := m.form.handleKey(msg)
		switch {
		case done && m.formStack != nil:
			m.form, m.formStack = m.formStack, nil
		case done:
			m.mode, m.form = modeNormal, nil
			m.resumeProto()
		case m.form.adding > 0:
			m.formStack = m.form
			return m, m.openMassAdd(m.addPrefill(m.form))
		}
		return m, cmd

	case modeConfirm:
		c := m.confirm
		switch msg.String() {
		case "enter", "y", "Y":
			m.mode, m.confirm = modeNormal, confirmState{}
			if c.Then != nil {
				c.Then()
			}
			return m, c.Do
		case "n", "N", "esc", "ctrl+c":
			m.mode, m.confirm = c.Back, confirmState{}
			if c.Back == modeNormal {
				m.setStatus("cancelled")
			}
		}
		// Any other key does nothing, so a stray key cannot confirm or cancel.
		return m, nil

	case modePalette:
		return m.handlePaletteKey(msg)

	case modeProto:
		return m.handleProtoKey(msg)

	case modePrompts:
		return m.handleModalKey(msg)

	case modeGlyph:
		return m.handleGlyphKey(msg)

	case modeTree:
		return m.handleTreeKey(msg)
	}

	var keep bool
	if msg, keep = m.remap(config.ModeDeck, msg, m.page == pageDeck); !keep {
		return m, nil
	}

	// Keys every page answers.
	switch msg.String() {
	case "?":
		return m, m.openHelp()
	case "ctrl+f":
		m.page, m.easyOnly = pageTasks, !m.easyOnly
		m.rebuildTasks()
		return m, nil
	case "F":
		return m, m.openFocus()
	case "q", "ctrl+c":
		m.confirm = confirmState{Text: "quit the deck? the panes stay (Y/n)", Do: tea.Quit}
		m.mode = modeConfirm
		return m, nil
	case ":", "ctrl+p":
		return m, m.openPalette()
	case "V":
		return m, m.openReviews()
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
	case "U":
		m.openProtos()
		return m, nil
	case "H":
		m.openHistory()
		return m, nil
	}
	if m.page == pageHistory {
		return m.handleHistoryKey(msg)
	}
	if m.page == pageReviews {
		return m.handleReviewsKey(msg)
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
	if m.page == pageProtos {
		return m.handleProtosKey(msg)
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
	case "j", "down", "k", "up", "g", "home", "{", "}", "(", ")", "tab", "]", "shift+tab", "[", "1", "2", "3", "4", "r", "enter":
		m.clearResult() // moving the selection or the tab ends a verb's result view
	}
	switch msg.String() {
	case "j", "down":
		m.move(1)
		return m, m.loadDetail(true)
	case "k", "up":
		m.move(-1)
		return m, m.loadDetail(true)
	case "{", "}":
		m.railJump(map[string]int{"{": -1, "}": 1}[msg.String()])
		return m, m.loadDetail(true)
	case "(", ")":
		m.railNest(map[string]int{"(": -1, ")": 1}[msg.String()])
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
	case "r":
		// Keep the lines and the scroll until the fresh read replaces them;
		// revision 0 makes sure it does.
		m.revision = 0
		return m, tea.Batch(m.refreshList(), m.loadDetail(true))
	case "enter":
		return m, m.openLive()
	case "o":
		m.cycleGrouping(1)
		return m, nil
	case "O":
		m.cycleGrouping(-1)
		return m, nil
	case "s":
		m.setRailSort((m.railSort + 1) % railSortCount)
		return m, nil
	case "R":
		m.railSort = railDefault
		m.setGrouping(grouping{lvSession})
		m.setStatus("view reset: session grouping, default sort")
		return m, nil
	case "n":
		return m, m.openAgentForm()
	case "t":
		return m, m.openTerminalForm()
	case "p":
		return m, m.openPromptForm()
	case "a":
		return m, m.openAssign()
	case "w":
		return m, m.openTree()
	case "c":
		return m, m.captureToFile()
	case "i":
		return m, m.extractContext()
	case "z":
		if p := m.selected(); p != nil {
			if a, ok := m.store.Assignment(p.ID); ok && a.TaskID != "" {
				return m, m.toggleTaskSnooze(a.TaskID)
			}
		}
		m.setStatus("select a pane assigned to a task to snooze it")
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

// cancelForm closes the form without submitting: back to the parked form
// when the mass-add screen was opened from one, else to the deck.
func (m *Model) cancelForm() {
	if m.formStack != nil {
		m.form, m.formStack = m.formStack, nil
		m.form.adding = 0
		m.mode = modeForm
		return
	}
	m.mode, m.form = modeNormal, nil
	m.setStatus("cancelled")
	m.resumeProto()
}

// confirmClose asks before closing the pane.
func (m *Model) confirmClose(p *panes.Pane) tea.Cmd {
	m.mode = modeConfirm
	m.confirm.Text = "close " + p.Name + " in " + p.Session + "? (Y/n)"
	m.confirm.Do = m.closeWindow(p.Session, p.ID, p.Name)
	return nil
}

// confirmCloseStale lists every stale pane on the right and asks before
// closing them all. The list is the one shown: a pane that turns stale
// while the question is open is not closed with it.
func (m *Model) confirmCloseStale() tea.Cmd {
	stale := panes.Stale(m.merged)
	if len(stale) == 0 {
		if m.settings.StaleThreshold() == 0 {
			m.setStatus("no stale panes: stale_after is 0, so none are marked")
		} else {
			m.setStatus("no stale panes: none idle for " + m.settings.StaleThreshold().String())
		}
		return nil
	}
	m.page = pageDeck
	m.result = &verbResult{Verb: "close stale panes", Params: fmt.Sprintf("%d idle for %s or more", len(stale), m.settings.StaleThreshold())}
	m.detail = nil
	for _, p := range stale {
		m.detail = append(m.detail, fmt.Sprintf("%-6s %s / %s", panes.Age(time.Duration(p.IdleMS)*time.Millisecond), p.Session, p.Name))
	}
	m.detailErr = ""
	m.scroll, m.follow, m.revision = 0, false, 0
	m.mode = modeConfirm
	m.confirm.Text = fmt.Sprintf("close the %d stale panes listed? (y/n)", len(stale))
	m.confirm.Then = m.clearResult
	m.confirm.Do = func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
		defer cancel()
		var failed []string
		for _, p := range stale {
			if err := m.client.CloseWindow(ctx, p.Session, p.ID); err != nil {
				failed = append(failed, p.Name+": "+err.Error())
			}
		}
		if len(failed) > 0 {
			return actionMsg{err: fmt.Errorf("closed %d of %d stale panes; %s", len(stale)-len(failed), len(stale), strings.Join(failed, "; "))}
		}
		return actionMsg{status: fmt.Sprintf("closed %d stale panes", len(stale))}
	}
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
		{Label: "cwd", Keep: true},
	}
	fields = append(fields, m.targetFields(false)...)
	fields = append(fields, m.presetField("its prefix goes in front of the first prompt, its cwd is used when cwd is empty"))
	fields = append(fields, field{Label: "first prompt", Keep: true, Multi: true, Prompt: true})
	m.mode = modeForm
	m.form = &form{
		Title:  "new agent",
		Fields: fields,
		Validate: func(v []string) (int, string) {
			if _, msg := m.resolvePreset(v[6]); msg != "" {
				return 6, msg
			}
			_, _, f, msg := m.resolveTarget(v[4], v[5], 4, 5, false)
			return f, msg
		},
		Done: func(v []string) tea.Cmd {
			taskID, projectID, _, _ := m.resolveTarget(v[4], v[5], 4, 5, false)
			pr, _ := m.resolvePreset(v[6])
			return m.startAgent(v[0], v[1], v[2], presetCwd(v[3], pr, p), presetPrefix(pr, v[7]), taskID, projectID)
		},
	}
	f := m.form
	f.Fields[3].HintFn = m.cwdHint(f, 6, p)
	m.cwdKey(f, "cwd")
	f.cwdFn = func() string {
		pr, _ := m.resolvePreset(f.Fields[6].result())
		return expandHome(presetCwd(f.Fields[3].result(), pr, p))
	}
	f.enableAttach(7, func() string { return strings.TrimSpace(f.Fields[1].result()) })
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
		{Label: "cwd", Keep: true},
	}
	fields = append(fields, m.targetFields(false)...)
	fields = append(fields, m.presetField("its cwd is used when cwd is empty"))
	m.mode = modeForm
	m.form = &form{
		Title:  "new terminal",
		Fields: fields,
		Validate: func(v []string) (int, string) {
			if _, msg := m.resolvePreset(v[5]); msg != "" {
				return 5, msg
			}
			_, _, f, msg := m.resolveTarget(v[3], v[4], 3, 4, false)
			return f, msg
		},
		Done: func(v []string) tea.Cmd {
			taskID, projectID, _, _ := m.resolveTarget(v[3], v[4], 3, 4, false)
			pr, _ := m.resolvePreset(v[5])
			return m.newTerminal(v[0], v[1], presetCwd(v[2], pr, p), taskID, projectID)
		},
	}
	m.form.Fields[2].HintFn = m.cwdHint(m.form, 5, p)
	m.cwdKey(m.form, "cwd")
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
		Cur:    1, // the text, not the preset: shift+tab reaches the preset
		Fields: []field{m.presetField("its prefix goes in front of the text"), {Label: label, Keep: true, Multi: true, Prompt: true}},
		Validate: func(v []string) (int, string) {
			if _, msg := m.resolvePreset(v[0]); msg != "" {
				return 0, msg
			}
			return 0, ""
		},
		Done: func(v []string) tea.Cmd {
			if v[1] == "" {
				return nil
			}
			pr, _ := m.resolvePreset(v[0])
			return m.sendPrompt(p.Session, p.ID, p.Name, p.IsAgent, presetPrefix(pr, v[1]))
		},
	}
	if p.IsAgent {
		m.form.enableAttach(1, func() string { return p.Harness })
	}
	return nil
}
