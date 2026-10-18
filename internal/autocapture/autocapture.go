// Package autocapture saves a pane's recent output to the captures folder
// on its own: when an agent's turn ends (it leaves working for idle,
// needs_input, done or errored) and when a shell command finishes under
// shell integration. It is a handler of the process's event stream
// (internal/events) and never touches a front end's view.
package autocapture

import (
	"context"
	"sync"
	"time"

	"github.com/unixsuperhero/tuios-deck/internal/capture"
	"github.com/unixsuperhero/tuios-deck/internal/daemon"
	"github.com/unixsuperhero/tuios-deck/internal/events"
	"github.com/unixsuperhero/tuios-deck/internal/panes"
)

// Gap is the least time between two captures of one window. Triggers that
// arrive sooner are collapsed into one capture at the end of the gap.
const Gap = 2 * time.Second

const callTimeout = 10 * time.Second

// turnEnds are the states that end a turn when the agent was working.
var turnEnds = map[string]bool{"idle": true, "needs_input": true, "done": true, "errored": true}

var eventTypes = []string{
	"agent-state", "command-finished", "window-created", "window-closed",
	"window-retitled", "session-closed",
}

// pane is what the watcher keeps per window.
type pane struct {
	session string
	state   string      // last agent state seen
	last    time.Time   // when the last capture was taken
	rev     int64       // revision of the last capture written
	kind    string      // a pending capture's kind, "" when none
	timer   *time.Timer // a capture scheduled for the end of the gap
}

// watcher is the handler. The dispatcher calls Handle and Resync one at a
// time; the gap timers fire on their own goroutines, so mu guards the panes.
type watcher struct {
	client *daemon.Client
	dir    string
	log    func(string)
	mu     sync.Mutex
	ctx    context.Context // the dispatcher's, for the timers' captures
	panes  map[string]*pane
	names  map[string]string // window id → pane name
	stale  bool              // names need a fresh listing
}

// New is the auto-capture handler for every session, writing captures to
// dir and reporting each one, and each failure, through log (which may be nil).
func New(client *daemon.Client, dir string, log func(string)) events.Handler {
	if log == nil {
		log = func(string) {}
	}
	return &watcher{client: client, dir: dir, log: log, ctx: context.Background(),
		panes: map[string]*pane{}, names: map[string]string{}, stale: true}
}

func (w *watcher) Types() []string { return eventTypes }

// Resync reads the panes again: a fresh stream, or one that lost events,
// may have missed windows coming and going.
func (w *watcher) Resync(ctx context.Context, _ string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.ctx = ctx
	w.stale = true
	w.list(ctx)
}

// list reads the panes once when the names are stale: their names, and
// the agent states a turn end is measured from.
func (w *watcher) list(ctx context.Context) {
	if !w.stale {
		return
	}
	lctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	sessions, err := panes.List(lctx, w.client)
	if err != nil {
		return
	}
	w.stale = false
	w.names = map[string]string{}
	for _, s := range sessions {
		for _, p := range s.Panes {
			w.names[p.ID] = p.Name
			pp := w.pane(s.Name, p.ID)
			if pp.state == "" {
				pp.state = p.State
			}
		}
	}
}

func (w *watcher) pane(session, win string) *pane {
	p := w.panes[win]
	if p == nil {
		p = &pane{}
		w.panes[win] = p
	}
	if session != "" {
		p.session = session
	}
	return p
}

func (w *watcher) Handle(ctx context.Context, ev daemon.Event) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.ctx = ctx
	if ev.Window == "" {
		if ev.Type == "session-closed" {
			w.stale = true
		}
		return
	}
	switch ev.Type {
	case "agent-state":
		p := w.pane(ev.Session, ev.Window)
		prev := p.state
		p.state = ev.State
		if prev == "working" && turnEnds[ev.State] {
			w.trigger(ctx, ev.Window, "turn")
		}
	case "command-finished":
		w.pane(ev.Session, ev.Window)
		w.trigger(ctx, ev.Window, "cmd")
	case "window-closed":
		if p := w.panes[ev.Window]; p != nil && p.timer != nil {
			p.timer.Stop()
		}
		delete(w.panes, ev.Window)
		delete(w.names, ev.Window)
	default: // created, retitled
		w.stale = true
	}
}

// trigger asks for a capture of win: now when the gap since the last one
// has passed, else once at the end of the gap. A turn outranks a command
// when both wait.
func (w *watcher) trigger(ctx context.Context, win, kind string) {
	p := w.panes[win]
	if p.kind != "turn" {
		p.kind = kind
	}
	if p.timer != nil {
		return
	}
	wait := time.Until(p.last.Add(Gap))
	if wait <= 0 {
		w.capture(ctx, win)
		return
	}
	p.timer = time.AfterFunc(wait, func() { w.fired(win) })
}

// fired is the end of a gap: the capture the timer waited for.
func (w *watcher) fired(win string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.ctx.Err() != nil {
		return
	}
	w.capture(w.ctx, win)
}

// capture writes the pane's whole recent buffer, unless nothing changed
// since the last capture written for it.
func (w *watcher) capture(ctx context.Context, win string) {
	p := w.panes[win]
	if p == nil || p.kind == "" {
		return
	}
	kind := p.kind
	p.kind, p.timer, p.last = "", nil, time.Now()
	cctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	c, err := w.client.CapturePane(cctx, p.session, win, 0, false)
	if err != nil {
		w.log("auto-capture " + p.session + " " + panes.ShortID(win) + ": " + err.Error())
		return
	}
	if p.rev != 0 && c.Revision == p.rev {
		return
	}
	if _, ok := w.names[win]; !ok {
		w.stale = true
	}
	w.list(ctx)
	name := w.names[win]
	if name == "" {
		name = panes.ShortID(win)
	}
	path, err := capture.WriteKind(w.dir, p.session, name, kind, c.Content)
	if err != nil {
		w.log("auto-capture: " + err.Error())
		return
	}
	p.rev = c.Revision
	w.log("auto-captured " + kind + " to " + path)
}
