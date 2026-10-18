package ui

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/unixsuperhero/tuios-deck/internal/focus"
	"github.com/unixsuperhero/tuios-deck/internal/store"
)

type focusScope struct {
	taskID, projectID string
	clock             focus.Clock
}

func (m *Model) focusTask(id string) bool {
	if m.focus == nil {
		return true
	}
	if m.focus.taskID == "" {
		return m.store.ProjectOf(id) == m.focus.projectID
	}
	for _, t := range m.store.Path(id) {
		if t.ID == m.focus.taskID {
			return true
		}
	}
	return false
}

func (m *Model) focusPane(id string) bool {
	if m.focus == nil {
		return true
	}
	a, ok := m.store.Assignment(id)
	if !ok {
		return false
	}
	if a.TaskID != "" {
		return m.focusTask(a.TaskID)
	}
	return m.focus.taskID == "" && a.ProjectID == m.focus.projectID
}

func (m *Model) focusTarget() (string, store.Theme) {
	if m.focus != nil {
		if t := m.store.Task(m.focus.taskID); t != nil {
			return t.Title, t.Theme
		}
		if p := m.store.Project(m.focus.projectID); p != nil {
			return p.Name, p.Theme
		}
	}
	return "", store.Theme{}
}

func (m *Model) startFocus(taskID, projectID string) tea.Cmd {
	m.focus = &focusScope{taskID: taskID, projectID: projectID, clock: focus.New(time.Now())}
	m.layoutSize()
	m.page = pageDeck
	m.easyOnly = false
	m.clearResult()
	m.detailFor, m.detailErr, m.status = "", "", ""
	m.detail, m.scroll, m.revision = nil, 0, 0
	m.turns, m.expanded = nil, map[string]bool{}
	m.rebuildRows()
	m.rebuildTasks()
	return m.loadDetail(true)
}

func (m *Model) unlockFocus() tea.Cmd {
	m.focus = nil
	m.layoutSize()
	m.rebuildRows()
	m.rebuildTasks()
	m.setStatus("focus unlocked")
	return nil
}

func (m *Model) openFocus() tea.Cmd {
	if m.focus != nil {
		return m.unlockFocus()
	}
	if m.page == pageTasks {
		if r := m.selectedRow(); r != nil {
			if r.Task != nil {
				return m.startFocus(r.Task.ID, "")
			}
			return m.startFocus("", r.Project.ID)
		}
	}
	m.protoRun = protoRun{purpose: purposeFocus, title: "focus task or project"}
	m.protoShots = nil
	m.proto = &treePicker{}
	m.proto.open(m, purposeFocus)
	m.mode = modeProto
	return nil
}

func (m *Model) layoutSize() {
	m.width, m.height = m.screenWidth, m.screenHeight
	if m.focus != nil {
		m.width, m.height = max(0, m.width-2), max(0, m.height-2)
	}
}

func (m *Model) focusScreen(text string) string {
	if m.focus == nil {
		return text
	}
	lines := strings.Split(text, "\n")
	lines = lines[:min(len(lines), m.height)]
	for i := range lines {
		lines[i] = pad(lines[i], m.width)
	}
	for len(lines) < m.height {
		lines = append(lines, strings.Repeat(" ", m.width))
	}
	return lipgloss.NewStyle().Border(lipgloss.NormalBorder()).Render(strings.Join(lines, "\n"))
}

func (m *Model) focusActivity(now time.Time, user bool) {
	if m.focus == nil {
		return
	}
	if m.focus.clock.Expired(now) {
		m.unlockFocus()
		return
	}
	if user {
		m.focus.clock.Activity(now)
	}
}

func (m *Model) focusBufferKey(key string) (bool, tea.Cmd) {
	if m.focus == nil || m.focus.clock.BufferUntil.IsZero() {
		return false, nil
	}
	switch key {
	case "enter":
		m.focus.clock = focus.New(time.Now())
	case "F", "esc":
		return true, m.unlockFocus()
	case "q", "ctrl+c":
		return true, tea.Quit
	}
	return true, nil
}

func (m *Model) focusBufferView() string {
	seconds := max(0, int(time.Until(m.focus.clock.BufferUntil).Seconds()+0.999))
	text := fmt.Sprintf("15 active minutes — continue focus?\n\nEnter continues for a fresh cycle.\nF or Esc unlocks now.\n\nUnlocking in %d seconds.\n\n%s", seconds, keyHelpHint)
	return m.focusScreen(lipgloss.NewStyle().Width(m.width).Height(m.height).Align(lipgloss.Center, lipgloss.Center).Render(text))
}
