package ui

import (
	"fmt"
	"regexp"
	"strconv"
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
	options := func() []string {
		var out []string
		for _, p := range m.store.Projects() {
			out = append(out, "project: "+p.Name)
		}
		for _, t := range m.store.Tasks() {
			out = append(out, "task: "+m.taskPath(t))
		}
		return out
	}
	resolve := func(v string) (string, string) {
		for _, p := range m.store.Projects() {
			if v == "project: "+p.Name {
				return "", p.ID
			}
		}
		for _, t := range m.store.Tasks() {
			if v == "task: "+m.taskPath(t) {
				return t.ID, ""
			}
		}
		return "", ""
	}
	m.mode = modeForm
	m.form = &form{Title: "focus task or project", Fields: []field{{Label: "scope", Options: options}},
		Validate: func(v []string) (int, string) {
			t, p := resolve(v[0])
			if t == "" && p == "" {
				return 0, "choose a task or project"
			}
			return 0, ""
		}, Done: func(v []string) tea.Cmd { t, p := resolve(v[0]); return m.startFocus(t, p) },
	}
	return nil
}

func (m *Model) focusKeyAllowed(key string) bool {
	if m.focus == nil {
		return true
	}
	// Only scoped navigation and actions are available; global pickers expose unrelated data.
	return strings.Contains("|?|F|q|ctrl+c|T|esc|j|k|up|down|h|l|left|right|g|G|home|end|J|K|ctrl+d|ctrl+u|pgdown|pgup|tab|shift+tab|[|]|1|2|3|4|r|enter|space|y|{|}|(|)|p|c|s|", "|"+key+"|")
}

var focusSGR = regexp.MustCompile("\x1b\\[[0-9;]*m")

func (m *Model) focusScreen(text string) string {
	if m.focus == nil {
		return text
	}
	_, theme := m.focusTarget()
	fg := "0"
	if theme.Color == "black" || theme.Color == "blue" || theme.Color == "red" || theme.Color == "magenta" {
		fg = "15"
	}
	// Keep bold, reverse selection and underlines, but remove embedded colours.
	text = focusSGR.ReplaceAllStringFunc(text, func(s string) string {
		parts := strings.Split(s[2:len(s)-1], ";")
		kept := parts[:0]
		for i := 0; i < len(parts); i++ {
			n, _ := strconv.Atoi(parts[i])
			if n == 38 || n == 48 || n == 58 {
				if i+1 < len(parts) && parts[i+1] == "2" {
					i += 4
				} else {
					i += 2
				}
			} else if n < 30 || n > 49 && n < 90 {
				kept = append(kept, parts[i])
			}
		}
		if len(kept) == 0 {
			return ""
		}
		return "\x1b[" + strings.Join(kept, ";") + "m"
	})
	return lipgloss.NewStyle().Foreground(lipgloss.Color(fg)).Background(lipgloss.Color(theme.ANSI())).Render(text)
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
	text := fmt.Sprintf("15 active minutes — continue focus?\n\nEnter continues for a fresh cycle.\nF or Esc unlocks now.\n\nUnlocking in %d seconds.", seconds)
	return m.focusScreen(lipgloss.NewStyle().Width(m.width).Height(m.height).Align(lipgloss.Center, lipgloss.Center).Render(text))
}
