package ui

import (
	"context"
	"errors"

	tea "charm.land/bubbletea/v2"

	"github.com/unixsuperhero/tuios-deck/internal/daemon"
)

// The pane's view in its own tuios session. The stream's lease never makes
// a pane larger than the tile the session's attached client draws it in, so
// while a tuios client shows the session the deck can only ask that client
// for more room: a zoom (ToggleZoom, which wants tiling on) or a fullscreen
// snap (SnapFullscreen, refused while tiling is on), through run-command.
// The deck sets the tiling mode each one needs before the command and puts
// the mode back when the view is undone. With no client attached there is
// nothing to ask and nothing to gain: the fit lease already sizes the pane
// to the deck, and run-command answers needs_client.

type paneView int

const (
	viewNormal paneView = iota
	viewZoom
	viewFullscreen
)

func (v paneView) String() string { return [...]string{"normal", "zoomed", "fullscreen"}[v] }

// tilingFor is the tiling mode a view needs, as session-info names it.
var tilingFor = map[paneView]string{viewZoom: "tiling", viewFullscreen: "floating"}

// tilingCommand is the tape command that sets a tiling mode.
var tilingCommand = map[string]string{"tiling": "EnableTiling", "floating": "DisableTiling"}

// paneViewState is what the deck changed in a pane's session, so it can be
// undone: the view, and the tiling mode the session had before the deck set
// the one the view needs ("" when it already had it).
type paneViewState struct {
	session, id string
	view        paneView
	tiling      string
}

type liveViewMsg struct {
	l     *live
	state paneViewState
	err   error
}

// togglePaneView asks the pane's session for the view, or for the tile back
// when the pane already has it. The calls run in order in one goroutine; a
// press while one runs is refused rather than queued.
func (m *Model) togglePaneView(want paneView) tea.Cmd {
	l := m.live
	if l.viewBusy {
		m.setStatus("still changing the pane's view")
		return nil
	}
	if l.view.view == want {
		want = viewNormal
	}
	return m.paneViewCmd(l, l.view, paneViewState{session: l.session, id: l.id, view: want})
}

// paneViewCmd undoes from and applies to, and tells l the outcome.
func (m *Model) paneViewCmd(l *live, from, to paneViewState) tea.Cmd {
	l.viewBusy = true
	client := m.client
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
		defer cancel()
		st, err := setPaneView(ctx, client, from, to)
		return liveViewMsg{l, st, err}
	}
}

// setPaneView takes the session from one view to another and returns the
// state in force afterwards, which on an error is as far as it got.
func setPaneView(ctx context.Context, c *daemon.Client, from, to paneViewState) (paneViewState, error) {
	if err := leavePaneView(ctx, c, from); err != nil {
		return from, err
	}
	normal := paneViewState{session: to.session, id: to.id}
	if to.view == viewNormal {
		return normal, nil
	}
	if err := c.FocusWindow(ctx, to.session, to.id); err != nil {
		return normal, err
	}
	mode, err := c.SessionTiling(ctx, to.session)
	if err != nil {
		return normal, err
	}
	if need := tilingFor[to.view]; mode != need {
		if err := c.RunCommand(ctx, to.session, tilingCommand[need]); err != nil {
			return normal, err
		}
		to.tiling = mode
	}
	cmd := map[paneView]string{viewZoom: "ToggleZoom", viewFullscreen: "SnapFullscreen"}[to.view]
	if err := c.RunCommand(ctx, to.session, cmd); err != nil {
		if to.tiling != "" {
			_ = c.RunCommand(ctx, to.session, tilingCommand[to.tiling])
		}
		return normal, err
	}
	return to, nil
}

// leavePaneView puts a pane back in its tile and the session back in the
// tiling mode it had. A zoom the person already ended in tuios is not
// toggled again.
func leavePaneView(ctx context.Context, c *daemon.Client, from paneViewState) error {
	switch from.view {
	case viewZoom:
		zoomed, err := c.WindowZoomed(ctx, from.session, from.id)
		if err != nil {
			return err
		}
		if zoomed {
			if err := c.FocusWindow(ctx, from.session, from.id); err != nil {
				return err
			}
			if err := c.RunCommand(ctx, from.session, "ToggleZoom"); err != nil {
				return err
			}
		}
	case viewFullscreen:
		if err := c.FocusWindow(ctx, from.session, from.id); err != nil {
			return err
		}
		if err := c.RunCommand(ctx, from.session, "unsnap"); err != nil {
			return err
		}
	}
	if from.tiling != "" {
		return c.RunCommand(ctx, from.session, tilingCommand[from.tiling])
	}
	return nil
}

// paneViewStatus is the status line for a view change, or its error.
func (m *Model) paneViewStatus(msg liveViewMsg) string {
	l := msg.l
	leader := m.liveBinding("leader")
	if msg.err != nil {
		var de *daemon.Error
		if errors.As(msg.err, &de) && de.Code == "needs_client" {
			return "no tuios client shows " + l.session + ", so there is no " + msg.state.view.String() + " view to ask for; the lease already fits " + l.name + " to the deck"
		}
		return "error: " + msg.err.Error()
	}
	switch msg.state.view {
	case viewZoom:
		return l.name + " is zoomed in " + l.session + " (tiling on); " + leader + " " + m.liveBinding("zoom_pane") + " puts it back"
	case viewFullscreen:
		return l.name + " is fullscreen in " + l.session + " (tiling off); " + leader + " " + m.liveBinding("fullscreen_pane") + " puts it back"
	}
	return l.name + " is back in its tile"
}
