package ui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/unixsuperhero/tuios-deck/internal/capture"
	"github.com/unixsuperhero/tuios-deck/internal/daemon"
	"github.com/unixsuperhero/tuios-deck/internal/panes"
)

type (
	helloMsg struct {
		info daemon.Hello
		err  error
	}
	listMsg struct {
		sessions []panes.Session
		err      error
	}
	detailMsg struct {
		key      string
		lines    []string
		revision int64
		err      error
		turns    []turn // the Turns tab's rows, when isTurns
		isTurns  bool
		empty    string // what the Turns tab says when it has no rows
	}
	eventMsg        daemon.Event
	eventsClosedMsg struct{ ch <-chan daemon.Event }
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
		status    string
		err       error
		selectRow string // a project or task id the tasks page selects after the change
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
		sessions, err := panes.List(ctx, m.client)
		return listMsg{sessions: sessions, err: err}
	}
}

// loadDetail reads the selected pane for the current tab. With force it runs
// even when a read is in flight; the tick calls it without force.
func (m *Model) loadDetail(force bool) tea.Cmd {
	if m.railReveal != "" && m.selectedID() != m.railReveal {
		m.railReveal = ""
		m.rebuildRows()
	}
	if m.result != nil {
		return nil // a verb's result holds the pane until the selection or the tab moves
	}
	p := m.selected()
	if p == nil {
		m.detailFor = ""
		return nil
	}
	key := p.ID + "/" + m.tab.String()
	if m.detailFor != key {
		m.detailFor = key
		m.scroll, m.follow = 0, true
		m.turns, m.turnSel, m.expanded = nil, 0, map[string]bool{}
		force = true
	}
	if m.detailBusy && !force {
		return nil
	}
	m.detailBusy = true
	tab, session, id, nonce := m.tab, p.Session, p.ID, m.nonce
	nonceErr := m.nonceErr
	if tab == tabTurns {
		return m.loadTurns(key, session, id, p.Cwd, p.IsAgent, nonce)
	}
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
			return detailMsg{key: key, lines: panes.ActivityLines(a)}
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
				var derr *daemon.Error
				if errors.As(err, &derr) && derr.Code == "no_transcript" {
					return detailMsg{key: key, lines: []string{"(this pane is not joined to an agent transcript)", "", derr.Message}}
				}
				return detailMsg{key: key, err: err}
			}
			return detailMsg{key: key, lines: panes.TranscriptLines(t)}
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
			"window-retitled", "session-created", "session-closed", "command-started", "command-finished",
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
			return eventsClosedMsg{ch}
		}
		return eventMsg(ev)
	}
}

func (m *Model) presence() tea.Cmd {
	ctx, cancel := context.WithCancel(context.Background())
	m.presenceCtx = cancel
	return func() tea.Msg {
		nonce, lost, err := m.client.Presence(ctx)
		if err == nil {
			go func() { <-lost }() // the connection closes itself; nothing to do here
		}
		return presenceMsg{nonce, err}
	}
}

// startAgent opens an agent pane and, once the daemon names the window,
// assigns it to the task or project the form gave.
func (m *Model) startAgent(session, agent, name, cwd, prompt, taskID, projectID string) tea.Cmd {
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
		s := res.Name + " (" + panes.ShortID(res.WindowID) + ")"
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
		if res.Session == "" {
			res.Session = session
		}
		s += m.store.RecordSubmission(res.Session, res.WindowID, prompt, res.PromptStatus)
		return m.assign(res.WindowID, res.Session, taskID, projectID, s)
	}
}

// newTerminal opens an interactive shell pane, the same pane tuios itself
// opens, so it stays up between the commands sent to it with p. The new
// window is assigned to the task or project the form gave.
func (m *Model) newTerminal(session, name, cwd, taskID, projectID string) tea.Cmd {
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
		res, err := m.client.NewWindow(ctx, session, name, cwd, nil)
		if err != nil {
			return actionMsg{err: err}
		}
		return m.assign(res.WindowID, session, taskID, projectID, "opened "+res.Name+" ("+panes.ShortID(res.WindowID)+") in "+session)
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
			warning := m.store.RecordSubmission(session, id, text, "queued")
			if res.Delivering {
				return actionMsg{status: "prompt delivering to " + name + warning}
			}
			return actionMsg{status: fmt.Sprintf("prompt queued for %s at position %d", name, res.Position) + warning}
		}
		if err := m.client.SendText(ctx, session, id, text+"\n"); err != nil {
			return actionMsg{err: err}
		}
		return actionMsg{status: "sent to " + name + m.store.RecordSubmission(session, id, text, "terminal")}
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
