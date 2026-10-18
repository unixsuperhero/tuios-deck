package ui

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/unixsuperhero/tuios-deck/internal/panes"
	"github.com/unixsuperhero/tuios-deck/internal/store"
)

type promptRecovery struct {
	entries  []store.StashedPrompt
	matches  []int
	query    string
	selected int
	scroll   int
}

func (p *promptRecovery) search(query string) {
	p.query, p.selected, p.scroll = query, 0, 0
	p.matches = p.matches[:0]
	query = strings.ToLower(query)
	for i, entry := range p.entries {
		if strings.Contains(strings.ToLower(entry.Text), query) {
			p.matches = append(p.matches, i)
		}
	}
}

func (m *Model) stashCurrentPrompt() bool {
	if err := m.store.StashPrompt(m.form.Fields[m.form.Cur].Value); err != nil {
		m.form.Err = "prompt not saved; draft unchanged: " + err.Error()
		m.setStatus(m.form.Err)
		return false
	}
	return true
}

func (m *Model) openPromptRecovery() {
	p := &promptRecovery{entries: m.store.StashedPrompts()}
	p.search("")
	m.form.recovery = p
	m.form.Err = ""
}

func (m *Model) promptRecoveryKey(msg tea.KeyPressMsg) {
	f, p := m.form, m.form.recovery
	switch msg.String() {
	case "esc", "ctrl+c":
		f.recovery = nil
		f.Err = ""
	case "up", "ctrl+p":
		p.selected, p.scroll = max(0, p.selected-1), 0
	case "down", "ctrl+n":
		p.selected, p.scroll = max(0, min(len(p.matches)-1, p.selected+1)), 0
	case "pgup":
		p.scroll = max(0, p.scroll-5)
	case "pgdown":
		p.scroll += 5
	case "backspace":
		p.search(dropLastGrapheme(p.query))
	case "ctrl+u":
		p.search("")
	case "enter":
		if len(p.matches) == 0 {
			return
		}
		text := p.entries[p.matches[p.selected]].Text
		cur := &f.Fields[f.Cur]
		if text != cur.Value {
			if !m.stashCurrentPrompt() {
				return
			}
			before := cur.editState()
			cur.Value = text
			cur.at, cur.sl, cur.hl, cur.top = 0, 0, 0, 0
			cur.pos = len(text)
			cur.remember(before)
		}
		f.recovery, f.Err = nil, ""
		m.setStatus("prompt restored; ctrl+z undoes")
	default:
		k := msg.Key()
		if k.Text != "" && k.Mod&^(tea.ModShift|tea.ModCapsLock|tea.ModNumLock|tea.ModScrollLock) == 0 {
			p.search(p.query + k.Text)
		}
	}
}

func (m *Model) promptRecoveryBox(width, maxH int) []string {
	p := m.form.recovery
	iw, avail := max(1, width-4), max(1, maxH-4)
	body := []string{ansi.Truncate("Search: "+p.query+"▏", iw, "…")}
	if m.form.Err != "" {
		body = append(body, ansi.Truncate(m.form.Err, iw, "…"))
	}
	if len(p.entries) == 0 {
		body = append(body, "No stashed prompts. Ctrl+U saves before clearing a line.")
	} else if len(p.matches) == 0 {
		body = append(body, "No matching stashed prompts.")
	} else {
		rows := min(5, max(1, (avail-len(body)-2)/2), len(p.matches))
		start := max(0, p.selected-rows+1)
		for i := start; i < min(len(p.matches), start+rows); i++ {
			entry := p.entries[p.matches[i]]
			label := time.Unix(entry.Saved, 0).Format("01-02 15:04") + "  " + panes.OneLine(ansi.Strip(entry.Text))
			mark := "  "
			if i == p.selected {
				mark = "▸ "
			}
			line := ansi.Truncate(mark+label, iw, "…")
			if i == p.selected {
				line = cSel.Render(pad(line, iw))
			}
			body = append(body, line)
		}
		text := p.entries[p.matches[p.selected]].Text
		preview := strings.Split(ansi.Hardwrap(ansi.Strip(text), iw, true), "\n")
		room := max(0, avail-len(body)-1)
		p.scroll = min(p.scroll, max(0, len(preview)-max(1, room)))
		body = append(body, cDim.Render(fmt.Sprintf("Preview %d/%d · lines %d–%d/%d", p.selected+1, len(p.matches), p.scroll+1, min(len(preview), p.scroll+room), len(preview))))
		body = append(body, preview[p.scroll:min(len(preview), p.scroll+room)]...)
	}
	lines := []string{boxTop("stashed prompts", width)}
	for _, line := range body[:min(len(body), avail)] {
		lines = append(lines, boxRow(ansi.Truncate(line, iw, "…"), iw))
	}
	return append(lines,
		cBorder.Render("├"+strings.Repeat("─", max(0, width-2))+"┤"),
		boxRow(cDim.Render(ansi.Truncate(keyHelpHint, iw, "…")), iw),
		cBorder.Render("╰"+strings.Repeat("─", max(0, width-2))+"╯"),
	)
}
