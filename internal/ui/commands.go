package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/unixsuperhero/tuios-deck/internal/capture"
	"github.com/unixsuperhero/tuios-deck/internal/daemon"
)

type (
	helloMsg struct {
		info daemon.Hello
		err  error
	}
	listMsg struct {
		sessions []daemon.Session
		agents   []daemon.Agent
		err      error
	}
	detailMsg struct {
		key      string
		lines    []string
		revision int64
		err      error
	}
	eventMsg        daemon.Event
	eventsClosedMsg struct{}
	resubscribeMsg  struct{}
	subscribedMsg   struct {
		ch  <-chan daemon.Event
		err error
	}
	presenceMsg struct {
		nonce string
		err   error
	}
	actionMsg struct {
		status string
		err    error
	}
	tickMsg time.Time
)

const callTimeout = 10 * time.Second

func tick() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func (m *Model) hello() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
		defer cancel()
		info, err := m.client.Hello(ctx, m.version)
		return helloMsg{info, err}
	}
}

// refreshList lists sessions and agents. One listing runs at a time; a
// request during one marks the model dirty so it runs again after.
func (m *Model) refreshList() tea.Cmd {
	if m.listBusy {
		m.dirty = true
		return nil
	}
	m.listBusy = true
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
		defer cancel()
		sessions, err := m.client.ListSessions(ctx)
		if err != nil {
			return listMsg{err: err}
		}
		agents, err := m.client.ListAgents(ctx)
		if err != nil {
			return listMsg{err: err}
		}
		return listMsg{sessions: sessions, agents: agents}
	}
}

// loadDetail reads the selected pane for the current tab. With force it runs
// even when a read is in flight; the tick calls it without force.
func (m *Model) loadDetail(force bool) tea.Cmd {
	p := m.selected()
	if p == nil {
		m.detailFor = ""
		return nil
	}
	key := p.ID + "/" + m.tab.String()
	if m.detailFor != key {
		m.detailFor = key
		m.scroll, m.follow = 0, true
		force = true
	}
	if m.detailBusy && !force {
		return nil
	}
	m.detailBusy = true
	tab, session, id, nonce := m.tab, p.Session, p.ID, m.nonce
	nonceErr := m.nonceErr
	lines := m.detailHeight() * 4
	if lines < 200 {
		lines = 200
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
		defer cancel()
		switch tab {
		case tabScreen:
			c, err := m.client.CapturePane(ctx, session, id, lines, true)
			if err != nil {
				return detailMsg{key: key, err: err}
			}
			return detailMsg{key: key, lines: strings.Split(strings.TrimRight(c.Content, "\n"), "\n"), revision: c.Revision}
		case tabActivity:
			a, err := m.client.Activity(ctx, session, id, 256)
			if err != nil {
				return detailMsg{key: key, err: err}
			}
			return detailMsg{key: key, lines: renderActivity(a)}
		default:
			if nonce == "" {
				msg := "transcript needs the person's presence, which the daemon refused"
				if nonceErr != "" {
					msg += ": " + nonceErr
				}
				return detailMsg{key: key, lines: []string{msg, "", "Run the deck outside a tuios pane to read transcripts. Activity (2) works from anywhere."}}
			}
			t, err := m.client.Transcript(ctx, session, id, nonce, 400)
			if err != nil {
				return detailMsg{key: key, err: err}
			}
			return detailMsg{key: key, lines: renderTranscript(t)}
		}
	}
}

func (m *Model) subscribe() tea.Cmd {
	if m.eventsCtx != nil {
		m.eventsCtx()
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.eventsCtx = cancel
	return func() tea.Msg {
		ch, err := m.client.Subscribe(ctx, "", []string{
			"agent-state", "attention", "window-created", "window-closed", "window-exit",
			"window-retitled", "session-created", "session-closed",
		})
		return subscribedMsg{ch: ch, err: err}
	}
}

func (m *Model) waitEvent() tea.Cmd {
	ch := m.events
	if ch == nil {
		return nil
	}
	return func() tea.Msg {
		ev, ok := <-ch
		if !ok {
			return eventsClosedMsg{}
		}
		return eventMsg(ev)
	}
}

func (m *Model) presence() tea.Cmd {
	ctx, cancel := context.WithCancel(context.Background())
	m.presenceCtx = cancel
	return func() tea.Msg {
		nonce, err := m.client.Presence(ctx)
		return presenceMsg{nonce, err}
	}
}

func (m *Model) startAgent(session, agent, name, cwd, prompt string) tea.Cmd {
	m.setStatus("starting " + agent + " in " + session + " (waits for its prompt)")
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
		defer cancel()
		res, err := m.client.StartAgent(ctx, daemon.StartAgentParams{
			Session: session, Agent: agent, Name: name, Cwd: cwd, Prompt: prompt,
		})
		if err != nil {
			return actionMsg{err: err}
		}
		s := res.Name + " (" + shortID(res.WindowID) + ")"
		if res.Ready {
			s += " is ready: " + res.State
		} else {
			s += " is not ready: " + res.Reason
			if res.BlockedBy != "" {
				s += " (" + res.BlockedBy + ")"
			}
		}
		if res.PromptStatus != "" {
			s += "; prompt " + res.PromptStatus
			if res.PromptNote != "" {
				s += " (" + res.PromptNote + ")"
			}
		}
		return actionMsg{status: s}
	}
}

func (m *Model) newTerminal(session, name, cwd string, command []string) tea.Cmd {
	width, height := m.width, m.height
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
		defer cancel()
		sessions, err := m.client.ListSessions(ctx)
		if err != nil {
			return actionMsg{err: err}
		}
		exists := false
		for _, s := range sessions {
			if s.Name == session {
				exists = true
			}
		}
		if !exists {
			if err := m.client.NewSession(ctx, session, cwd, width, height); err != nil {
				return actionMsg{err: err}
			}
		}
		res, err := m.client.NewWindow(ctx, session, name, cwd, command)
		if err != nil {
			return actionMsg{err: err}
		}
		return actionMsg{status: "opened " + res.Name + " (" + shortID(res.WindowID) + ") in " + session}
	}
}

func (m *Model) sendPrompt(session, id, name string, isAgent bool, text string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
		defer cancel()
		if isAgent {
			res, err := m.client.QueuePrompt(ctx, session, id, text)
			if err != nil {
				return actionMsg{err: err}
			}
			if res.Delivering {
				return actionMsg{status: "prompt delivering to " + name}
			}
			return actionMsg{status: fmt.Sprintf("prompt queued for %s at position %d", name, res.Position)}
		}
		if err := m.client.SendText(ctx, session, id, text+"\n"); err != nil {
			return actionMsg{err: err}
		}
		return actionMsg{status: "sent to " + name}
	}
}

func (m *Model) captureToFile() tea.Cmd {
	p := m.selected()
	if p == nil {
		return nil
	}
	session, id, name, dir := p.Session, p.ID, p.Name, m.captureDir
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
		defer cancel()
		c, err := m.client.CapturePane(ctx, session, id, 0, false)
		if err != nil {
			return actionMsg{err: err}
		}
		path, err := capture.Write(dir, session, name, c.Content)
		if err != nil {
			return actionMsg{err: err}
		}
		return actionMsg{status: "captured to " + path}
	}
}

func (m *Model) closeWindow(session, id, name string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
		defer cancel()
		if err := m.client.CloseWindow(ctx, session, id); err != nil {
			return actionMsg{err: err}
		}
		return actionMsg{status: "closed " + name}
	}
}

func renderActivity(a daemon.Activity) []string {
	var out []string
	r := a.Recap
	out = append(out, fmt.Sprintf("state %s · turns %d · commands %d · files %d", r.State, r.Turns, r.Commands, r.FilesTotal))
	if r.LastSaid != "" {
		out = append(out, "last said: "+oneLine(r.LastSaid))
	}
	if len(r.Files) > 0 {
		out = append(out, "files: "+strings.Join(r.Files, ", "))
	}
	out = append(out, "")
	if len(a.Entries) == 0 {
		out = append(out, "(no activity yet: the pane reports no hook events)")
	}
	for _, e := range a.Entries {
		ts := time.Unix(0, e.At).Format("15:04:05")
		var line string
		switch e.Kind {
		case "prompt":
			line = "you> " + oneLine(e.Text)
		case "tool":
			line = "→ " + e.Tool + " " + oneLine(e.Target)
		case "tool_done":
			line = "  ok " + e.Tool
		case "tool_failed":
			line = "  FAILED " + e.Tool + " " + oneLine(e.Text)
		case "turn_end":
			line = "■ turn ended " + oneLine(e.Text)
		case "command":
			line = "$ " + oneLine(e.Target)
			if e.Exit != nil {
				line += fmt.Sprintf(" (exit %d)", *e.Exit)
			}
		default:
			line = e.Kind + " " + oneLine(e.Text)
		}
		out = append(out, ts+" "+line)
	}
	return out
}

func renderTranscript(t daemon.Transcript) []string {
	var out []string
	for _, e := range t.Entries {
		ts := time.UnixMilli(e.At).Format("15:04:05")
		switch e.Kind {
		case "tool_call":
			out = append(out, fmt.Sprintf("%s → %s %s", ts, e.Tool, oneLine(e.Target)))
		case "tool_result":
			if e.Status == "error" {
				out = append(out, fmt.Sprintf("%s   FAILED %s", ts, oneLine(e.Text)))
			}
		case "thinking":
			continue
		default:
			prefix := e.Role + ">"
			if e.Role == "user" {
				prefix = "you>"
			}
			first := true
			for _, l := range strings.Split(strings.TrimRight(e.Text, "\n"), "\n") {
				if first {
					out = append(out, ts+" "+prefix+" "+l)
					first = false
				} else {
					out = append(out, "         "+l)
				}
			}
			out = append(out, "")
		}
	}
	if len(out) == 0 {
		out = append(out, "(empty transcript for harness "+t.Harness+")")
	}
	return out
}

func oneLine(s string) string {
	s = strings.ReplaceAll(strings.TrimSpace(s), "\n", " ")
	if len(s) > 200 {
		s = s[:200] + "…"
	}
	return s
}
