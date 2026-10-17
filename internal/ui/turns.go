package ui

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/unixsuperhero/tuios-deck/internal/daemon"
	"github.com/unixsuperhero/tuios-deck/internal/panes"
)

// turn is a row of the Turns tab: a prompt the person gave an agent, with
// the agent's reply, or a command a shell ran.
type turn struct {
	ID     string
	At     time.Time
	Cmd    bool     // a shell command rather than a prompt
	Head   string   // the collapsed row after the time
	Body   []string // the lines shown when expanded
	Text   string   // what y copies: the prompt, or the command line
	source string   // where the rows came from, for the empty state
}

// loadTurns reads the rows of the Turns tab. An agent's prompts come from
// its transcript when the deck holds the person's presence, else from its
// activity ring; every pane's commands come from command-history.
func (m *Model) loadTurns(key, session, id, paneCwd string, isAgent bool, nonce string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
		defer cancel()
		var rows []turn
		source := ""
		if isAgent {
			var err error
			rows, source, err = m.agentTurns(ctx, session, id, nonce)
			if err != nil {
				return detailMsg{key: key, err: err}
			}
		}
		cmds, err := m.client.CommandHistory(ctx, session, id, 200)
		if err != nil && !isAgent {
			return detailMsg{key: key, err: err}
		}
		looked := false
		for _, c := range cmds {
			if c.Cwd == "" && paneCwd == "" && !looked {
				paneCwd, _ = m.client.WindowCwd(ctx, session, id)
				looked = true
			}
			rows = append(rows, commandTurn(c, paneCwd))
		}
		sort.SliceStable(rows, func(i, j int) bool { return rows[i].At.Before(rows[j].At) })
		if len(rows) == 0 {
			rows = nil
			source = "no prompts" + source + ", and no commands from its shell"
			if !isAgent {
				source = "no commands from this shell yet (a shell reports commands only with OSC 133 marks)"
			}
		}
		return detailMsg{key: key, turns: rows, isTurns: true, empty: source}
	}
}

// agentTurns reads the person's prompts and the replies between them from
// the transcript, or the prompts alone from agent-activity when there is no
// presence or no transcript. The second result names the source for the
// empty state.
func (m *Model) agentTurns(ctx context.Context, session, id, nonce string) ([]turn, string, error) {
	if nonce != "" {
		t, err := m.client.Transcript(ctx, session, id, nonce, 1000)
		var derr *daemon.Error
		switch {
		case err == nil:
			return transcriptTurns(t), " in the transcript", nil
		case errors.As(err, &derr) && (derr.Code == "no_transcript" || derr.Code == "not_human"):
			// fall back to the activity ring below
		default:
			return nil, "", err
		}
	}
	a, err := m.client.Activity(ctx, session, id, 256)
	if err != nil {
		return nil, "", err
	}
	var rows []turn
	for _, e := range a.Entries {
		switch e.Kind {
		case "prompt":
			rows = append(rows, promptTurn(fmt.Sprintf("a%d", e.Seq), time.Unix(0, e.At), e.Text))
		case "turn_end":
			if n := len(rows); n > 0 && e.Text != "" {
				rows[n-1].Body = append(rows[n-1].Body, "", "  turn ended:")
				rows[n-1].Body = append(rows[n-1].Body, indent(e.Text)...)
			}
		}
	}
	return rows, " in the agent's activity", nil
}

// transcriptTurns makes a row of each person prompt, with the agent's text
// that followed it until the next prompt as the reply.
func transcriptTurns(t daemon.Transcript) []turn {
	var rows []turn
	replied := false
	for _, e := range t.Entries {
		if e.Kind != "text" {
			continue
		}
		switch e.Role {
		case "user":
			rows = append(rows, promptTurn(e.ID, time.UnixMilli(e.At), e.Text))
			replied = false
		case "assistant":
			n := len(rows)
			if n == 0 {
				continue
			}
			if !replied {
				rows[n-1].Body = append(rows[n-1].Body, "", "  reply:")
				replied = true
			}
			rows[n-1].Body = append(rows[n-1].Body, indent(e.Text)...)
		}
	}
	return rows
}

func promptTurn(id string, at time.Time, text string) turn {
	return turn{ID: id, At: at, Head: "you> " + panes.OneLine(firstLine(text)), Body: indent(text), Text: text}
}

// commandTurn makes a command's row. A shell that does not report the
// command's folder leaves the pane's present one, marked as such.
func commandTurn(c daemon.HistoryEntry, paneCwd string) turn {
	exit := "exit ?"
	if c.Exit != nil {
		exit = fmt.Sprintf("exit %d", *c.Exit)
	}
	dur := (time.Duration(c.DurationMS) * time.Millisecond).Round(100 * time.Millisecond)
	if c.DurationMS < 1000 {
		dur = time.Duration(c.DurationMS) * time.Millisecond
	}
	cwd := c.Cwd
	if cwd == "" && paneCwd != "" {
		cwd = paneCwd + "  (the pane's folder now; the shell did not report the command's)"
	} else if cwd == "" {
		cwd = "(the shell did not report it)"
	}
	body := []string{
		"  cwd  " + cwd,
		"  " + exit + " after " + dur.String() + ", started " + time.UnixMilli(c.At).Format("2006-01-02 15:04:05"),
		"  the daemon keeps no output for a command; the Screen tab (1) has the scrollback",
	}
	if c.Exit == nil {
		body[1] = "  still running, or its exit was not reported; started " + time.UnixMilli(c.At).Format("2006-01-02 15:04:05")
	}
	return turn{
		ID: fmt.Sprintf("c%d", c.Seq), At: time.UnixMilli(c.At), Cmd: true,
		Head: fmt.Sprintf("$ %-7s %6s  %s", exit, dur.String(), panes.OneLine(c.Cmdline)),
		Body: body, Text: c.Cmdline,
	}
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

func indent(s string) []string {
	var out []string
	for _, l := range strings.Split(strings.TrimRight(s, "\n"), "\n") {
		out = append(out, "    "+l)
	}
	return out
}

// turnsFingerprint says whether a fresh read differs from the rows shown:
// a row added or removed, or the newest row's reply grown.
func turnsFingerprint(rows []turn) string {
	if len(rows) == 0 {
		return "0"
	}
	last := rows[len(rows)-1]
	return fmt.Sprintf("%d/%s/%d", len(rows), last.ID, len(last.Body))
}

// applyTurns takes a fresh read of the Turns tab. Rows are replaced only
// when the read differs; the selection stays on the same row by id, or goes
// to the newest while following, and the expanded rows stay expanded.
func (m *Model) applyTurns(rows []turn, empty string) {
	m.turnsEmpty = empty
	if m.turns != nil && turnsFingerprint(rows) == turnsFingerprint(m.turns) {
		return
	}
	selID := ""
	if m.turnSel >= 0 && m.turnSel < len(m.turns) {
		selID = m.turns[m.turnSel].ID
	}
	m.turns = rows
	m.turnSel = len(rows) - 1
	if !m.follow {
		for i, r := range rows {
			if r.ID == selID {
				m.turnSel = i
			}
		}
	}
	m.renderTurns()
}

// renderTurns lays the rows out as the detail's lines, wrapped to the main
// area, and notes the line each row starts on.
func (m *Model) renderTurns() {
	width := m.width - m.leftWidth() - 1
	if width < 20 {
		width = 20
	}
	m.turnLine = m.turnLine[:0]
	var lines []string
	if len(m.turns) == 0 {
		lines = []string{cDim.Render("(" + m.turnsEmpty + ")")}
	}
	for i, r := range m.turns {
		m.turnLine = append(m.turnLine, len(lines))
		mark := "▸"
		if m.expanded[r.ID] {
			mark = "▾"
		}
		line := mark + " " + r.At.Format("15:04:05") + " " + r.Head
		switch {
		case i == m.turnSel && m.focusMain:
			line = cSel.Render(pad(line, width))
		case i == m.turnSel:
			line = cTitle.Render(line)
		}
		lines = append(lines, line)
		if m.expanded[r.ID] {
			for _, b := range r.Body {
				for _, w := range strings.Split(ansi.Hardwrap(b, width, true), "\n") {
					lines = append(lines, cDim.Render(w))
				}
			}
			lines = append(lines, "")
		}
	}
	m.detail = lines
	m.clampScroll()
}

// handleTurnsKey answers the keys of the Turns tab while the main area has
// focus: j/k move between rows, enter or space expand one, y copies it.
func (m *Model) handleTurnsKey(msg tea.KeyPressMsg) (bool, tea.Cmd) {
	switch msg.String() {
	case "j", "down":
		m.moveTurn(1)
	case "k", "up":
		m.moveTurn(-1)
	case "g", "home":
		m.turnSel = 0
		m.follow = false
		m.renderTurns()
		m.scroll = 0
		m.clampScroll()
	case "G", "end":
		m.turnSel = len(m.turns) - 1
		m.follow = true
		m.renderTurns()
	case "enter", "space", " ":
		if m.turnSel >= 0 && m.turnSel < len(m.turns) {
			id := m.turns[m.turnSel].ID
			m.expanded[id] = !m.expanded[id]
			m.renderTurns()
			m.showTurn()
		}
	case "y":
		if m.turnSel >= 0 && m.turnSel < len(m.turns) {
			text := m.turns[m.turnSel].Text
			m.setStatus("copied " + panes.OneLine(firstLine(text)) + " with OSC 52")
			return true, tea.SetClipboard(text)
		}
	default:
		return false, nil
	}
	return true, nil
}

// moveTurn moves the selection a row and keeps the row on screen.
func (m *Model) moveTurn(dir int) {
	if len(m.turns) == 0 {
		return
	}
	m.turnSel = max(0, min(len(m.turns)-1, m.turnSel+dir))
	m.follow = false
	m.renderTurns()
	m.showTurn()
}

// showTurn scrolls so the selected row's first line is visible.
func (m *Model) showTurn() {
	if m.turnSel < 0 || m.turnSel >= len(m.turnLine) {
		return
	}
	line, body := m.turnLine[m.turnSel], m.detailHeight()
	if line < m.scroll {
		m.scroll = line
	} else if line >= m.scroll+body {
		m.scroll = line - body + 1
	}
	m.clampScroll()
}
