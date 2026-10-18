// Package ui is the deck: a Bubble Tea program that lists every terminal and
// agent the tuios daemon holds, spawns new ones, sends prompts, and shows
// each pane's screen, activity and transcript.
package ui

import (
	"context"
	"os"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/unixsuperhero/tuios-deck/internal/capture"
	"github.com/unixsuperhero/tuios-deck/internal/config"
	"github.com/unixsuperhero/tuios-deck/internal/daemon"
	"github.com/unixsuperhero/tuios-deck/internal/panes"
	"github.com/unixsuperhero/tuios-deck/internal/store"
)

type mode int

const (
	modeNormal mode = iota
	modeForm
	modeConfirm
	modePalette
	modeProto // a prototype picker is up
)

// page is which view the deck shows: the deck of panes, the tasks tree or
// the captures folder.
type page int

const (
	pageDeck page = iota
	pageTasks
	pageCaptures
	pageContext
	pageProtos
)

func (p page) String() string {
	return [...]string{"deck", "tasks", "captures", "context", "prototypes"}[p]
}

type tab int

const (
	tabScreen tab = iota
	tabActivity
	tabTranscript
	tabTurns
	numTabs
)

func (t tab) String() string {
	return [...]string{"Screen", "Activity", "Transcript", "Turns"}[t]
}

// row is a line of the left list: a group header or a pane. A header
// grouped by session carries the session; one grouped by project or task
// carries its theme.
type row struct {
	Header  string
	Session *panes.Session
	Theme   *store.Theme
	Pane    *panes.Pane
	Depth   int // the nesting: a header's level, or one past the deepest header for a pane
}

// Model is the deck's Bubble Tea model.
type Model struct {
	client  *daemon.Client
	store   *store.Store
	version string

	settings     *config.Config // nil until SetSettings: the defaults
	settingsPath string
	settingsKeys map[string]map[string]config.Target // the rebinding tables, by mode

	width, height int

	page page

	merged   []panes.Session // the last listing, which o regroups without a daemon call
	grouping grouping
	rows     []row
	sel      int
	railUp   int // the levels ( has stepped up from the innermost, for { and }
	listErr  string
	dirty    bool
	listBusy bool

	trows  []trow // the tasks page
	tsel   int
	tselID string

	caps      []capture.Entry // the captures page
	csel      int
	cselName  string
	capLines  []string
	capErr    string
	capScroll int
	capFollow bool

	cx ctxPage // the context page

	protoSel   int              // the prototypes page's highlighted row
	proto      picker           // the prototype that is up
	protoBack  picker           // the prototype the mass-add screen put aside
	protoRun   protoRun         // what the prototype that is up drives
	protoShots map[string]*shot // the screens its preview read, by window id

	focusMain  bool // h and l move it: the rail, or the main area to the right
	tab        tab
	detail     []string
	detailErr  string
	detailFor  string // window id + tab the detail belongs to
	detailBusy bool
	scroll     int
	follow     bool
	revision   int64

	turns      []turn          // the Turns tab's rows, which the detail's lines are drawn from
	turnSel    int             // the focused row
	turnLine   []int           // the detail line each row starts on
	turnsEmpty string          // what the tab says when there are no rows
	expanded   map[string]bool // the expanded rows, by id, kept across re-reads

	mode mode
	form *form
	// formStack is the form suspended while the mass-add screen it opened
	// is up; it comes back when that screen is submitted or cancelled.
	formStack *form
	confirm   struct {
		Text string
		Do   tea.Cmd
	}

	pal        *palette // the command palette while open
	verbs      []verb   // the daemon's list-verbs, fetched on the first open
	verbsBusy  bool
	optionKeys []string // list-options paths, fetched when a form needs them
	themeNames []string // list-themes ids, likewise
	result     *verbResult

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

	previews previewState // the capture a form field's preview is showing
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
		expanded:   map[string]bool{},
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
		if m.tab == tabTurns && m.turns != nil {
			m.renderTurns()
		}
		return m, m.loadDetail(true)

	case tea.KeyPressMsg:
		mm, cmd := m.handleKey(msg)
		return mm, tea.Batch(cmd, m.previewCmd(false))

	case previewMsg:
		m.applyPreview(msg)
		return m, nil

	case tea.PasteMsg:
		if m.mode == modeForm && m.form != nil && !m.form.vimNormal {
			m.form.paste(msg.String())
		}
		if m.mode == modePalette && m.pal != nil {
			m.pal.setQuery(m.pal.Query + panes.OneLine(msg.String()))
		}
		if m.mode == modeProto && m.proto != nil {
			return m, m.protoPaste(panes.OneLine(msg.String()))
		}
		return m, nil

	case verbsMsg:
		m.verbsBusy = false
		if msg.err != nil {
			m.setStatus("error: list-verbs: " + msg.err.Error())
			return m, nil
		}
		m.verbs = msg.verbs
		m.rebuildPalette()
		return m, nil

	case namesMsg:
		if msg.err != nil {
			m.setStatus("error: " + msg.kind + ": " + msg.err.Error())
			return m, nil
		}
		if msg.kind == "options" {
			m.optionKeys = msg.names
		} else {
			m.themeNames = msg.names
		}
		return m, nil

	case resultMsg:
		m.showResult(msg)
		return m, m.refreshList()

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
		if msg.key != m.detailFor || m.result != nil {
			return m, nil // the selection moved while this one was in flight
		}
		if msg.err != nil {
			m.detailErr = msg.err.Error()
		} else if msg.isTurns {
			m.detailErr = ""
			m.applyTurns(msg.turns, msg.empty)
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

	case previewMsg:
		m.takePreview(msg)
		return m, nil

	case actionMsg:
		if msg.err != nil {
			m.setStatus("error: " + msg.err.Error())
		} else if msg.status != "" {
			m.setStatus(msg.status)
		}
		if msg.selectRow != "" {
			m.tselID = msg.selectRow
		}
		m.rebuildTasks()
		if m.page == pageCaptures {
			m.loadCaptures()
		}
		if m.page == pageContext {
			m.rebuildContext()
		}
		return m, m.refreshList()

	case tickMsg:
		var cmds []tea.Cmd
		if m.mode != modeConfirm && m.page == pageDeck {
			cmds = append(cmds, m.loadDetail(false))
		}
		cmds = append(cmds, m.previewCmd(true), tick())
		return m, tea.Batch(cmds...)
	}
	return m, nil
}

func (m *Model) setStatus(s string) {
	m.status = s
	m.statusAt = time.Now()
}

// applyList keeps the two listings merged, rebuilds the rows under the
// current grouping and keeps the selection on the same pane where it still
// exists.
func (m *Model) applyList(sessions []daemon.Session, agents []daemon.Agent) {
	m.merged = panes.Merge(sessions, agents)
	live := map[string]bool{}
	for _, s := range m.merged {
		for _, p := range s.Panes {
			live[p.ID] = true
		}
	}
	m.pruneAssignments(live)
	m.rebuildRows()
	m.rebuildTasks()
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
		if r.Session != nil {
			return r.Header
		}
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

// clearResult gives the detail pane back to the selected pane.
func (m *Model) clearResult() {
	if m.result != nil {
		m.result = nil
		m.detail, m.detailErr, m.revision = nil, "", 0
		m.scroll, m.follow = 0, true
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
