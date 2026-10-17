package ui

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/unixsuperhero/tuios-deck/internal/capture"
)

// openCaptures shows the captures page and reads the folder.
func (m *Model) openCaptures() {
	m.page = pageCaptures
	m.loadCaptures()
}

// loadCaptures re-reads the captures folder and the selected file. The
// selection stays on the same file name; when that file is gone it stays
// at the same place in the list. The scroll is kept while the selected
// file is the same one.
func (m *Model) loadCaptures() {
	entries, err := capture.List(m.captureDir)
	if err != nil {
		m.caps, m.capErr = nil, err.Error()
		return
	}
	m.capErr = ""
	m.caps = entries
	found := -1
	for i, e := range entries {
		if e.Name == m.cselName {
			found = i
			break
		}
	}
	if found < 0 {
		found = m.csel
		if found >= len(entries) {
			found = len(entries) - 1
		}
		if found < 0 {
			found = 0
		}
	}
	m.cselect(found)
}

// cselect moves the captures selection to i and reads that file. Moving
// to another file resets the scroll to follow; re-reading the same file
// keeps it.
func (m *Model) cselect(i int) {
	if i < 0 || i >= len(m.caps) {
		m.csel, m.cselName, m.capLines, m.capErr = -1, "", nil, ""
		return
	}
	if m.caps[i].Name != m.cselName {
		m.capScroll, m.capFollow = 0, true
	}
	m.csel, m.cselName = i, m.caps[i].Name
	content, err := capture.Read(m.captureDir, m.cselName)
	if err != nil {
		m.capLines, m.capErr = nil, err.Error()
		return
	}
	m.capErr = ""
	m.capLines = strings.Split(strings.TrimRight(content, "\n"), "\n")
	m.clampCapScroll()
}

func (m *Model) cmove(dir int) {
	if next := m.csel + dir; next >= 0 && next < len(m.caps) {
		m.cselect(next)
	}
}

func (m *Model) selectedCapture() *capture.Entry {
	if m.csel >= 0 && m.csel < len(m.caps) {
		return &m.caps[m.csel]
	}
	return nil
}

func (m *Model) clampCapScroll() {
	visible := m.detailHeight()
	max := len(m.capLines) - visible
	if max < 0 {
		max = 0
	}
	if m.capFollow || m.capScroll > max {
		m.capScroll = max
	}
	if m.capScroll < 0 {
		m.capScroll = 0
	}
}

func (m *Model) handleCapturesKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.page = pageDeck
		return m, m.loadDetail(true)
	case "j", "down":
		m.cmove(1)
	case "k", "up":
		m.cmove(-1)
	case "g", "home":
		m.cselect(0)
	case "G", "end":
		m.capFollow = true
		m.clampCapScroll()
	case "J", "ctrl+d", "pgdown":
		m.capFollow = false
		m.capScroll += m.detailHeight() / 2
		m.clampCapScroll()
	case "K", "ctrl+u", "pgup":
		m.capFollow = false
		m.capScroll -= m.detailHeight() / 2
		m.clampCapScroll()
	case "r":
		m.loadCaptures()
		m.setStatus(plural(len(m.caps), "capture"))
	case "x":
		if m.selectedCapture() == nil {
			return m, nil
		}
		m.confirmDeleteCapture()
	case "enter":
		e := m.selectedCapture()
		if e == nil {
			return m, nil
		}
		return m, m.jumpToCapturedPane(*e)
	}
	return m, nil
}

// confirmDeleteCapture asks before deleting the selected capture's file.
func (m *Model) confirmDeleteCapture() {
	e := m.selectedCapture()
	name, dir := e.Name, m.captureDir
	m.mode = modeConfirm
	m.confirm.Text = "delete " + name + "? (y/N)"
	m.confirm.Do = func() tea.Msg {
		if err := capture.Remove(dir, name); err != nil {
			return actionMsg{err: err}
		}
		return actionMsg{status: "deleted " + name}
	}
}

// jumpToCapturedPane goes back to the deck page with the pane the capture
// came from selected, when a pane of that session and name is still
// listed.
func (m *Model) jumpToCapturedPane(e capture.Entry) tea.Cmd {
	for i, r := range m.rows {
		if r.Pane != nil && e.Matches(r.Pane.Session, r.Pane.Name) {
			m.sel = i
			m.page = pageDeck
			m.detail, m.detailErr, m.revision = nil, "", 0
			m.follow = true
			m.setStatus(r.Pane.Session + " › " + r.Pane.Name)
			return m.loadDetail(true)
		}
	}
	m.setStatus("no pane " + e.Pane + " in " + e.Session + " is open now")
	return nil
}

// captureTime is hh:mm for a file written today, else its date.
func captureTime(t time.Time) string {
	now := time.Now()
	if t.Year() == now.Year() && t.YearDay() == now.YearDay() {
		return t.Format("15:04")
	}
	return t.Format("2006-01-02")
}

func captureSize(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1fM", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1fK", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%dB", n)
}

// captureListLines draws the files of the captures page, newest first:
// the pane name, its session and the kind of an automatic capture, when it
// was written and how big it is.
func (m *Model) captureListLines(width, height int) []string {
	lines := make([]string, 0, height)
	if m.capErr != "" && len(m.caps) == 0 {
		lines = append(lines, cErr.Render(ansi.Truncate(m.capErr, width, "…")))
	}
	if len(m.caps) == 0 && m.capErr == "" {
		lines = append(lines, cDim.Render("no captures: press c on a pane"))
	}
	start := 0
	if m.csel >= height {
		start = m.csel - height + 1
	}
	for i := start; i < len(m.caps) && len(lines) < height; i++ {
		e := m.caps[i]
		right := captureTime(e.Modified) + " " + cDim.Render(pad(captureSize(e.Size), 6))
		session := e.Session
		if e.Kind != "" {
			session += " " + e.Kind
		}
		session = cDim.Render(session)
		room := width - 1 - ansi.StringWidth(right) - 2 - ansi.StringWidth(session) - 1
		if room < 4 {
			room = 4
		}
		name := ansi.Truncate(e.Pane, room, "…")
		fill := width - 1 - ansi.StringWidth(name) - 2 - ansi.StringWidth(session) - ansi.StringWidth(right)
		if fill < 1 {
			fill = 1
		}
		line := " " + name + "  " + session + strings.Repeat(" ", fill) + right
		if i == m.csel {
			line = cSel.Render(ansi.Strip(pad(line, width)))
		}
		lines = append(lines, line)
	}
	for len(lines) < height {
		lines = append(lines, "")
	}
	return lines
}

// captureDetailLines shows the selected capture on the right: its file on
// the first line, then the content at the scroll position.
func (m *Model) captureDetailLines(width, height int) []string {
	lines := make([]string, 0, height)
	e := m.selectedCapture()
	if e == nil {
		lines = append(lines, cDim.Render(" no capture selected"))
		for len(lines) < height {
			lines = append(lines, "")
		}
		return lines
	}
	bar := cTabOn.Render(" "+e.Name+" ") + "  " + cDim.Render(ansi.Truncate(e.Path, width-ansi.StringWidth(e.Name)-5, "…"))
	lines = append(lines, bar)
	body := height - 1
	if m.capErr != "" {
		lines = append(lines, cErr.Render(ansi.Truncate(m.capErr, width, "…")))
	} else {
		end := m.capScroll + body
		if end > len(m.capLines) {
			end = len(m.capLines)
		}
		for _, l := range m.capLines[m.capScroll:end] {
			lines = append(lines, ansi.Truncate(l, width, ""))
		}
	}
	for len(lines) < height {
		lines = append(lines, "")
	}
	if !m.capFollow && len(m.capLines) > body {
		lines[height-1] = pad(lines[height-1], width-12) + cDim.Render(fmt.Sprintf("%4d/%d ↓G", m.capScroll+1, len(m.capLines)))
	}
	return lines
}
