package ui

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/unixsuperhero/tuios-deck/internal/config"
	"github.com/unixsuperhero/tuios-deck/internal/daemon"
)

// live is the deck gone live on one pane: every key the person types goes
// to the pane through the daemon, and the pane's screen is read back each
// time it prints. The deck embeds no terminal emulator, so the daemon's own
// does the work: capture-pane (visible, styled) renders the screen,
// get-window reports the cursor, and send-keys encodes each key for the
// pane's modes. One stream-pane connection is held open for two things
// only: its size lease, which fits the pane to the deck's live area, and
// its frames, which say when the pane printed something and when it ended.
//
// Input keeps its order: one goroutine drains the queue and makes the
// daemon calls one after another, since tea.Cmds run concurrently and would
// let a later key arrive first. Runs of typed text go as one send-text.
type live struct {
	session, id, name string
	zoom              bool
	prefixed          bool // the prefix was pressed; the next key is a command
	screen            []string
	curX, curY        int
	cursor            bool
	cols, rows        int // the pane's size, from the stream's S and R frames

	cancel context.CancelFunc
	in     chan liveInput
	wake   chan struct{} // the pane printed; the capturer reads it again
	out    chan tea.Msg  // what the goroutines tell the model

	mu     sync.Mutex
	stream *daemon.Stream
	leased [2]int // the lease held, renewed every ten seconds; asked for before the stream opened, it is sent then
}

// liveInput is one item of the input queue: text to type, a key name for
// send-keys, or a paste.
type liveInput struct {
	text  string
	keys  string
	paste bool
}

type (
	liveScreenMsg struct {
		l          *live
		lines      []string
		curX, curY int
		cursor     bool
	}
	liveSizeMsg struct {
		l          *live
		cols, rows int
	}
	liveEndMsg struct {
		l      *live
		reason string
	}
	liveErrMsg struct {
		l   *live
		err error
	}
)

var cLive = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("0")).Background(lipgloss.Color("2"))

// liveBinding is the key of a live action, configured or default.
func (m *Model) liveBinding(name string) string {
	if m.settings != nil {
		return m.settings.Binding(config.ModeLive, name)
	}
	return (&config.Config{}).Binding(config.ModeLive, name)
}

// liveSize is the area the live screen is drawn in: the detail pane below
// its live bar, or the whole body when zoomed. It is what View draws, so
// the lease and the drawing agree.
func (m *Model) liveSize(zoom bool) (cols, rows int) {
	rows = m.height - 3 - 1 // title, status and help lines, and the live bar
	cols = m.width
	if !zoom {
		left := min(m.leftWidth(), m.width-2)
		cols = m.width - left - 1
	}
	return max(1, min(cols, 4096)), max(1, min(rows, 4096))
}

// openLive goes live on the selected pane. A live session already open is
// closed first, keeping its zoom, which is how the prefix's j and k move.
func (m *Model) openLive() tea.Cmd {
	p := m.selected()
	if p == nil {
		m.setStatus("select a pane first")
		return nil
	}
	zoom := false
	if m.live != nil {
		zoom = m.live.zoom
		m.live.cancel()
	}
	m.clearResult()
	l := &live{
		session: p.Session, id: p.ID, name: p.Name, zoom: zoom,
		in: make(chan liveInput, 256), wake: make(chan struct{}, 1), out: make(chan tea.Msg, 64),
	}
	ctx, cancel := context.WithCancel(context.Background())
	l.cancel = cancel
	m.live = l
	m.detail, m.detailErr, m.revision = nil, "", 0
	cols, rows := m.liveSize(zoom)
	go l.run(ctx, m.client, cols, rows)
	leader := m.liveBinding("leader")
	m.setStatus("live on " + p.Name + ": keys go to the pane; " + leader + " " + m.liveBinding("detach") + " comes back, " + leader + " " + m.liveBinding("help") + " lists the keys")
	return m.waitLive()
}

// closeLive ends the live session and gives the detail pane back.
func (m *Model) closeLive(status string) tea.Cmd {
	if m.live == nil {
		return nil
	}
	m.live.cancel()
	m.live = nil
	m.detail, m.detailErr, m.revision = nil, "", 0
	m.scroll, m.follow = 0, true
	if status != "" {
		m.setStatus(status)
	}
	return m.loadDetail(true)
}

// waitLive reads the next message of the live goroutines.
func (m *Model) waitLive() tea.Cmd {
	l := m.live
	if l == nil {
		return nil
	}
	return func() tea.Msg { return <-l.out }
}

// handleLiveKey takes every key while live: the prefix arms a command, the
// key after it runs one (or the prefix itself is typed), and anything else
// goes to the pane.
func (m *Model) handleLiveKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	l := m.live
	leader := m.liveBinding("leader")
	if !l.prefixed {
		if msg.String() == leader {
			l.prefixed = true
			return m, nil
		}
		l.send(liveKey(msg))
		return m, nil
	}
	l.prefixed = false
	if msg.String() == leader {
		l.send(liveKey(msg))
		return m, nil
	}
	var keep bool
	if msg, keep = m.remap(config.ModeLive, msg, true); !keep {
		return m, nil
	}
	switch msg.String() {
	case "d":
		return m, m.closeLive("detached from " + l.name + "; it keeps running")
	case "z":
		l.zoom = !l.zoom
		l.lease(m.liveSize(l.zoom))
		return m, nil
	case "j", "k":
		m.move(map[string]int{"j": 1, "k": -1}[msg.String()])
		if p := m.selected(); p != nil && p.ID == l.id {
			m.setStatus("no pane " + map[string]string{"j": "below", "k": "above"}[msg.String()])
			return m, nil
		}
		return m, m.openLive()
	case "?":
		return m, m.openHelp()
	case "esc":
		m.setStatus("prefix cancelled")
		return m, nil
	}
	m.setStatus(leader + " " + msg.String() + " is not a live command; " + leader + " " + m.liveBinding("help") + " lists them")
	return m, nil
}

// liveKeyNames are the keys Bubble Tea and the daemon spell differently.
var liveKeyNames = map[string]string{
	"esc": "Escape", "pgup": "PageUp", "pgdown": "PageDown", "space": "Space", "backspace": "Backspace",
	"delete": "Delete", "insert": "Insert", "enter": "Enter", "tab": "Tab",
}

// liveKey turns a key press into the input item that types it: printable
// text as text, anything else as a send-keys name with its modifiers.
func liveKey(msg tea.KeyPressMsg) liveInput {
	if msg.Text != "" && msg.Mod&(tea.ModCtrl|tea.ModAlt|tea.ModMeta|tea.ModSuper|tea.ModHyper) == 0 {
		return liveInput{text: msg.Text}
	}
	ks := msg.Keystroke()
	mods, name := "", ks
	if i := strings.LastIndex(ks, "+"); i > 0 && i < len(ks)-1 {
		mods, name = ks[:i+1], ks[i+1:]
	}
	if n, ok := liveKeyNames[name]; ok {
		name = n
	}
	return liveInput{keys: mods + name}
}

// send queues one input item. A full queue drops it rather than stall the
// deck; the daemon takes keys far faster than anyone types.
func (l *live) send(in liveInput) {
	select {
	case l.in <- in:
	default:
	}
}

// lease asks the daemon to hold the pane at this size, now or as soon as
// the stream is open.
func (l *live) lease(cols, rows int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.leased = [2]int{cols, rows}
	l.writeLease()
}

// renew sends the lease held again; a lease lasts thirty seconds.
func (l *live) renew() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.writeLease()
}

// writeLease sends the L frame for the lease held. Called with mu held.
func (l *live) writeLease() {
	if l.stream == nil || l.leased == [2]int{} {
		return
	}
	cols, rows := l.leased[0], l.leased[1]
	frame := make([]byte, 9)
	frame[0] = 'L'
	binary.BigEndian.PutUint32(frame[1:5], 4)
	binary.BigEndian.PutUint16(frame[5:7], uint16(cols))
	binary.BigEndian.PutUint16(frame[7:9], uint16(rows))
	l.stream.WriteFrame(frame)
}

func (l *live) tell(ctx context.Context, msg tea.Msg) {
	select {
	case l.out <- msg:
	case <-ctx.Done():
	}
}

// run is the live session's goroutines: the stream for the lease and the
// output signal, the capturer and the sender. It ends when ctx does.
func (l *live) run(ctx context.Context, client *daemon.Client, cols, rows int) {
	go l.sendLoop(ctx, client)
	period := time.Second // a safety re-read; the stream's frames drive the rest
	l.mu.Lock()
	if l.leased == [2]int{} {
		l.leased = [2]int{cols, rows}
	}
	cols, rows = l.leased[0], l.leased[1]
	l.mu.Unlock()
	st, err := client.StreamPane(ctx, daemon.StreamParams{Session: l.session, Window: l.id, LeaseCols: cols, LeaseRows: rows})
	if err != nil {
		// Without the stream the pane is not fitted and nothing says when it
		// printed, so the screen is polled instead.
		l.tell(ctx, liveErrMsg{l, err})
		period = 100 * time.Millisecond
	} else {
		l.tell(ctx, liveSizeMsg{l, st.Reply.Cols, st.Reply.Rows})
		l.mu.Lock()
		l.stream = st
		if l.leased != [2]int{cols, rows} {
			l.writeLease() // the size changed while the stream was opening
		}
		l.mu.Unlock()
		go l.readLoop(ctx, st)
		go l.renewLoop(ctx)
	}
	l.captureLoop(ctx, client, period)
	if st != nil {
		st.Close()
	}
}

// renewLoop sends the lease again every ten seconds, as the protocol asks.
func (l *live) renewLoop(ctx context.Context) {
	t := time.NewTicker(10 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			l.renew()
		}
	}
}

// readLoop reads the stream's frames: output and size frames wake the
// capturer, an E frame is reported, an X frame ends the session.
func (l *live) readLoop(ctx context.Context, st *daemon.Stream) {
	for {
		frame, err := st.ReadFrame()
		if err != nil {
			l.tell(ctx, liveEndMsg{l, "the stream closed"})
			return
		}
		switch frame[0] {
		case 'O', 'S', 'R':
			if frame[0] != 'O' && len(frame) >= 17 {
				l.tell(ctx, liveSizeMsg{l, int(binary.BigEndian.Uint16(frame[13:15])), int(binary.BigEndian.Uint16(frame[15:17]))})
			}
			select {
			case l.wake <- struct{}{}:
			default:
			}
		case 'E':
			var e daemon.Error
			if json.Unmarshal(frame[5:], &e) == nil && e.Code != "lease_expired" {
				l.tell(ctx, liveErrMsg{l, &e})
			}
		case 'X':
			l.tell(ctx, liveEndMsg{l, string(frame[5:])})
			return
		}
	}
}

// captureLoop reads the screen and the cursor whenever the pane printed,
// at most every 20 ms, and on a timer besides.
func (l *live) captureLoop(ctx context.Context, client *daemon.Client, period time.Duration) {
	t := time.NewTicker(period)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-l.wake:
		case <-t.C:
		}
		cctx, cancel := context.WithTimeout(ctx, callTimeout)
		c, err := client.CaptureScreen(cctx, l.session, l.id)
		w, werr := client.GetWindow(cctx, l.session, l.id)
		cancel()
		if err != nil {
			l.tell(ctx, liveErrMsg{l, err})
			continue
		}
		msg := liveScreenMsg{l: l, lines: strings.Split(strings.TrimRight(c.Content, "\n"), "\n")}
		if werr == nil && w.CursorX != nil && w.CursorY != nil {
			msg.curX, msg.curY, msg.cursor = *w.CursorX, *w.CursorY, w.CursorVisible
		}
		l.tell(ctx, msg)
		select {
		case <-ctx.Done():
			return
		case <-time.After(20 * time.Millisecond):
		}
	}
}

// sendLoop makes the daemon calls for the input queue, in order, joining
// a run of typed text into one send-text.
func (l *live) sendLoop(ctx context.Context, client *daemon.Client) {
	var next *liveInput
	for {
		var in liveInput
		if next != nil {
			in, next = *next, nil
		} else {
			select {
			case <-ctx.Done():
				return
			case in = <-l.in:
			}
		}
		if in.text != "" && !in.paste {
		more:
			for {
				select {
				case n := <-l.in:
					if n.text != "" && !n.paste {
						in.text += n.text
						continue
					}
					next = &n
					break more
				default:
					break more
				}
			}
		}
		cctx, cancel := context.WithTimeout(ctx, callTimeout)
		var err error
		switch {
		case in.paste:
			err = client.Paste(cctx, l.session, l.id, in.text)
		case in.text != "":
			err = client.SendText(cctx, l.session, l.id, in.text)
		default:
			err = client.SendKeys(cctx, l.session, l.id, in.keys)
		}
		cancel()
		if err != nil {
			l.tell(ctx, liveErrMsg{l, err})
		}
	}
}

// liveLines draws the live area: a bar naming the pane and the way back,
// then the pane's screen with its cursor in reverse video.
func (m *Model) liveLines(width, height int) []string {
	l := m.live
	leader := m.liveBinding("leader")
	bar := cLive.Render(" LIVE ") + " " + l.session + " › " + l.name
	if l.cols > 0 {
		bar += cDim.Render(fmt.Sprintf("  %d×%d", l.cols, l.rows))
	}
	hint := "  " + cDim.Render(leader+" "+m.liveBinding("detach")+" detaches")
	if l.prefixed {
		hint = "  " + cStatus.Render(leader+" … "+m.liveBinding("detach")+" detach · "+m.liveBinding("zoom")+" zoom · "+m.liveBinding("next_pane")+"/"+m.liveBinding("prev_pane")+" pane · "+m.liveBinding("help")+" keys · esc cancel")
	}
	lines := []string{ansi.Truncate(bar+hint, width, "…")}
	for i := 0; i < height-1; i++ {
		line := ""
		if i < len(l.screen) {
			line = l.screen[i]
		}
		if l.cursor && i == l.curY {
			line = cursorAt(line, l.curX)
		}
		lines = append(lines, ansi.Truncate(line, width, "")+"\x1b[0m")
	}
	return lines
}

// cursorAt draws the cell at column x of a styled row in reverse video.
func cursorAt(line string, x int) string {
	left := ansi.Truncate(line, x, "")
	if w := ansi.StringWidth(left); w < x {
		left += strings.Repeat(" ", x-w)
	}
	rest := ansi.TruncateLeft(line, x, "")
	cell := ansi.Strip(ansi.Truncate(rest, 1, ""))
	if cell == "" {
		cell = " "
	}
	return left + "\x1b[7m" + cell + "\x1b[27m" + ansi.TruncateLeft(rest, 1, "")
}

// liveHelpLine is the help line while live.
func (m *Model) liveHelpLine() string {
	leader := m.liveBinding("leader")
	return cLive.Render(" LIVE ") + cDim.Render(" keys go to "+m.live.name+" · "+leader+" "+m.liveBinding("detach")+" detach · "+leader+" "+m.liveBinding("zoom")+" zoom · "+leader+" "+m.liveBinding("next_pane")+"/"+m.liveBinding("prev_pane")+" other pane · "+leader+" "+leader+" types "+leader+" · "+leader+" "+m.liveBinding("help")+" keys")
}
