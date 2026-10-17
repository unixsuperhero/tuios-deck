// Package ui is the deck: a Bubble Tea program that lists every terminal and
// agent the tuios daemon holds, spawns new ones, sends prompts, and shows
// each pane's screen, activity and transcript.
package ui

import (
	"context"
	"os"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/unixsuperhero/tuios-deck/internal/daemon"
	"github.com/unixsuperhero/tuios-deck/internal/panes"
	"github.com/unixsuperhero/tuios-deck/internal/store"
)

type mode int

const (
	modeNormal mode = iota
	modeForm
	modeConfirm
)

type tab int

const (
	tabScreen tab = iota
	tabActivity
	tabTranscript
)

func (t tab) String() string {
	return [...]string{"Screen", "Activity", "Transcript"}[t]
}

// row is a line of the left list: a session header or a pane.
type row struct {
	Header  string
	Session *panes.Session
	Pane    *panes.Pane
}

// Model is the deck's Bubble Tea model.
type Model struct {
	client  *daemon.Client
	store   *store.Store
	version string

	width, height int

	rows     []row
	sel      int
	listErr  string
	dirty    bool
	listBusy bool

	tab        tab
	detail     []string
	detailErr  string
	detailFor  string // window id + tab the detail belongs to
	detailBusy bool
	scroll     int
	follow     bool
	revision   int64

	mode    mode
	form    *form
	confirm struct {
		Text string
		Do   tea.Cmd
	}

	status   string
	statusAt time.Time

	events    <-chan daemon.Event
	eventsCtx context.CancelFunc
	eventsErr string

	nonce       string
	nonceErr    string
	presenceCtx context.CancelFunc

	captureDir string
	daemonInfo string

	missing map[string]bool // assigned windows the last listing did not hold
}

// New builds a deck for one daemon socket, with the store that holds the
// projects, tasks and pane assignments.
func New(client *daemon.Client, st *store.Store, version, captureDir string) *Model {
	return &Model{
		client:     client,
		store:      st,
		version:    version,
		captureDir: captureDir,
		follow:     true,
		status:     "connecting to " + client.Socket,
	}
}

// Init starts the first listing, the event stream and the presence.
func (m *Model) Init() tea.Cmd {
	return tea.Batch(m.hello(), m.refreshList(), m.subscribe(), m.presence(), tick())
}

// Update is the message loop.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, m.loadDetail(true)

	case tea.KeyPressMsg:
		return m.handleKey(msg)

	case tea.PasteMsg:
		if m.mode == modeForm && m.form != nil {
			m.form.paste(msg.String())
		}
		return m, nil

	case helloMsg:
		if msg.err != nil {
			m.setStatus("daemon: " + msg.err.Error())
		} else {
			m.daemonInfo = "tuios " + msg.info.DaemonVersion
			m.status = ""
		}
		return m, nil

	case listMsg:
		m.listBusy = false
		if msg.err != nil {
			m.listErr = msg.err.Error()
		} else {
			m.listErr = ""
			m.applyList(msg.sessions, msg.agents)
		}
		var cmd tea.Cmd
		if m.dirty {
			m.dirty = false
			cmd = m.refreshList()
		}
		return m, tea.Batch(cmd, m.loadDetail(false))

	case detailMsg:
		m.detailBusy = false
		if msg.key != m.detailFor {
			return m, nil // the selection moved while this one was in flight
		}
		if msg.err != nil {
			m.detailErr = msg.err.Error()
		} else {
			m.detailErr = ""
			if msg.revision == 0 || msg.revision != m.revision {
				m.detail = msg.lines
				m.revision = msg.revision
			}
		}
		m.clampScroll()
		return m, nil

	case eventMsg:
		ev := daemon.Event(msg)
		if ev.Type == "agent-state" || ev.Type == "attention" {
			// A state change is what the person watches for; refresh at once.
			return m, tea.Batch(m.refreshList(), m.waitEvent())
		}
		return m, tea.Batch(m.refreshList(), m.waitEvent())

	case eventsClosedMsg:
		m.events = nil
		m.eventsErr = "event stream closed; reconnecting"
		return m, tea.Tick(2*time.Second, func(time.Time) tea.Msg { return resubscribeMsg{} })

	case resubscribeMsg:
		return m, m.subscribe()

	case subscribedMsg:
		if msg.err != nil {
			m.eventsErr = "events: " + msg.err.Error()
			return m, tea.Tick(5*time.Second, func(time.Time) tea.Msg { return resubscribeMsg{} })
		}
		m.eventsErr = ""
		m.events = msg.ch
		return m, m.waitEvent()

	case presenceMsg:
		if msg.err != nil {
			m.nonceErr = msg.err.Error()
		} else {
			m.nonce = msg.nonce
		}
		return m, nil

	case actionMsg:
		if msg.err != nil {
			m.setStatus("error: " + msg.err.Error())
		} else if msg.status != "" {
			m.setStatus(msg.status)
		}
		return m, m.refreshList()

	case tickMsg:
		var cmds []tea.Cmd
		if m.mode != modeConfirm {
			cmds = append(cmds, m.loadDetail(false))
		}
		cmds = append(cmds, tick())
		return m, tea.Batch(cmds...)
	}
	return m, nil
}

func (m *Model) setStatus(s string) {
	m.status = s
	m.statusAt = time.Now()
}

// applyList rebuilds the rows from the two listings and keeps the selection on
// the same pane where it still exists.
func (m *Model) applyList(sessions []daemon.Session, agents []daemon.Agent) {
	selected := m.selectedID()
	merged := panes.Merge(sessions, agents)
	var rows []row
	newSel := -1
	live := map[string]bool{}
	for i := range merged {
		s := &merged[i]
		rows = append(rows, row{Header: s.Name, Session: s})
		for j := range s.Panes {
			p := &s.Panes[j]
			if p.ID == selected {
				newSel = len(rows)
			}
			live[p.ID] = true
			rows = append(rows, row{Pane: p})
		}
	}
	m.rows = rows
	m.pruneAssignments(live)
	if newSel >= 0 {
		m.sel = newSel
	} else {
		m.sel = m.firstPane(0, 1)
	}
}

func (m *Model) selected() *panes.Pane {
	if m.sel >= 0 && m.sel < len(m.rows) {
		return m.rows[m.sel].Pane
	}
	return nil
}

func (m *Model) selectedID() string {
	if p := m.selected(); p != nil {
		return p.ID
	}
	return ""
}

// selectedSession is the session of the selected row, header or pane.
func (m *Model) selectedSession() string {
	if m.sel >= 0 && m.sel < len(m.rows) {
		r := m.rows[m.sel]
		if r.Pane != nil {
			return r.Pane.Session
		}
		return r.Header
	}
	return ""
}

// firstPane finds the next pane row from start in direction dir, or -1.
func (m *Model) firstPane(start, dir int) int {
	for i := start; i >= 0 && i < len(m.rows); i += dir {
		if m.rows[i].Pane != nil {
			return i
		}
	}
	return -1
}

func (m *Model) move(dir int) {
	if next := m.firstPane(m.sel+dir, dir); next >= 0 {
		m.sel = next
		m.detail, m.detailErr, m.revision = nil, "", 0
		m.follow = true
	}
}

func (m *Model) clampScroll() {
	visible := m.detailHeight()
	max := len(m.detail) - visible
	if max < 0 {
		max = 0
	}
	if m.follow || m.scroll > max {
		m.scroll = max
	}
	if m.scroll < 0 {
		m.scroll = 0
	}
}

func (m *Model) detailHeight() int {
	h := m.height - 4 // title, tab bar, status, help
	if h < 1 {
		h = 1
	}
	return h
}

func (m *Model) leftWidth() int {
	w := m.width / 3
	if w < 24 {
		w = 24
	}
	if w > 48 {
		w = 48
	}
	return w
}

func cwdDefault(p *panes.Pane) string {
	if p != nil && p.Cwd != "" {
		return p.Cwd
	}
	wd, _ := os.Getwd()
	return wd
}

// Close releases the event stream and the presence.
func (m *Model) Close() {
	if m.eventsCtx != nil {
		m.eventsCtx()
	}
	if m.presenceCtx != nil {
		m.presenceCtx()
	}
}
