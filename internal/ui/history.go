package ui

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/unixsuperhero/tuios-deck/internal/history"
	"github.com/unixsuperhero/tuios-deck/internal/panes"
	"github.com/unixsuperhero/tuios-deck/internal/store"
)

type historyPage struct {
	entries            []store.HistoryEntry
	selected           string
	index, top, scroll int
	err                string
}

func (m *Model) openHistory() { m.page = pageHistory; m.loadHistory() }

func (m *Model) loadHistory() {
	h := &m.history
	old := h.index
	h.entries, h.err = m.store.History()
	for i, e := range h.entries {
		if e.ID == h.selected {
			h.index = i
			h.top += i - old
			return
		}
	}
	m.selectHistory(0)
}

func (m *Model) selectHistory(i int) {
	h := &m.history
	if len(h.entries) == 0 {
		h.index, h.top, h.scroll, h.selected = 0, 0, 0, ""
		return
	}
	i = max(0, min(i, len(h.entries)-1))
	if h.selected != h.entries[i].ID {
		h.scroll = 0
	}
	h.index, h.selected = i, h.entries[i].ID
}

func (m *Model) handleHistoryKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	h := &m.history
	switch msg.String() {
	case "esc":
		m.page = pageDeck
		return m, m.loadDetail(true)
	case "j", "down":
		m.selectHistory(h.index + 1)
	case "k", "up":
		m.selectHistory(h.index - 1)
	case "g", "home":
		m.selectHistory(0)
	case "G", "end":
		m.selectHistory(len(h.entries) - 1)
	case "J", "pgdown", "ctrl+d":
		h.scroll += max(1, m.detailHeight()/2)
	case "K", "pgup", "ctrl+u":
		h.scroll = max(0, h.scroll-max(1, m.detailHeight()/2))
	case "r":
		m.loadHistory()
	case "enter":
		if len(h.entries) == 0 {
			break
		}
		e := h.entries[h.index]
		if m.revealPane(e.Window) {
			m.page = pageDeck
			m.clearResult()
			return m, m.loadDetail(true)
		}
		m.setStatus("the history source pane is not open now")
	}
	return m, nil
}

func (m *Model) historyListLines(width, height int) []string {
	out := make([]string, max(0, height))
	if height < 1 {
		return out
	}
	h := &m.history
	out[0] = cHeader.Render(fmt.Sprintf(" History · %d · newest first", len(h.entries)))
	if height < 2 {
		return out
	}
	visible := height - 1
	h.top = max(0, min(h.top, max(0, len(h.entries)-visible)))
	if h.index < h.top {
		h.top = h.index
	}
	if h.index >= h.top+visible {
		h.top = h.index - visible + 1
	}
	if len(h.entries) == 0 {
		out[1] = cDim.Render(" No observed history yet")
		return out
	}
	for i := h.top; i < len(h.entries) && i-h.top < visible; i++ {
		e := h.entries[i]
		row := time.UnixMilli(e.At).Format("01-02 15:04:05") + " " + panes.OneLine(e.Summary)
		if i == h.index {
			row = cSel.Render(pad(row, width))
		}
		out[1+i-h.top] = row
	}
	return out
}

func (m *Model) historyDetailLines(width, height int) []string {
	out := make([]string, max(0, height))
	h := &m.history
	text := history.Boundary
	if len(h.entries) > 0 {
		e := h.entries[h.index]
		text = fmt.Sprintf("%s\n%s\n\nSession: %s\nWindow: %s\nSource: %s\nIdentity: %s\nObserved: %s\n\n%s\n\n%s", e.Summary, time.UnixMilli(e.At).Format(time.RFC3339Nano), e.Session, e.Window, e.Source, e.SourceID, time.UnixMilli(e.Observed).Format(time.RFC3339Nano), e.Detail, history.Boundary)
	}
	if h.err != "" {
		text = "History error: " + h.err + "\n\n" + text
	}
	lines := strings.Split(ansi.Hardwrap(ansi.Strip(text), max(1, width), true), "\n")
	h.scroll = max(0, min(h.scroll, max(0, len(lines)-height)))
	copy(out, lines[h.scroll:])
	return out
}
