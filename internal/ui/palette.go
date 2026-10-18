package ui

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/unixsuperhero/tuios-deck/internal/daemon"
	"github.com/unixsuperhero/tuios-deck/internal/panes"
)

// verb is one entry of the daemon's list-verbs: the contract of one call.
type verb struct {
	Verb        string      `json:"verb"`
	Description string      `json:"description"`
	Params      []verbParam `json:"params"`
}

type verbParam struct {
	Name        string          `json:"name"`
	Type        string          `json:"type"`
	Required    bool            `json:"required"`
	Description string          `json:"description"`
	Accepted    []string        `json:"accepted"`
	Default     json.RawMessage `json:"default"`
}

// palItem is one entry of the command palette: a deck command or a tuios
// verb. Run does what picking it does; it may open a form.
type palItem struct {
	Cat  string // "deck" or "tuios"
	Name string
	Hint string
	Run  func() tea.Cmd
}

// palette is the command palette while it is open: the query typed so far,
// every entry, the entries the query keeps, and the highlighted one.
type palette struct {
	Query string
	all   []palItem
	shown []palItem
	sel   int
}

// verbResult names the verb whose result the detail pane shows in place
// of the selected pane, until the selection or the tab changes.
type verbResult struct {
	Verb   string
	Params string
}

type (
	verbsMsg struct {
		verbs []verb
		err   error
	}
	namesMsg struct {
		kind  string // "options" or "themes"
		names []string
		err   error
	}
	resultMsg struct {
		verb, params string
		raw          json.RawMessage
		err          error
	}
)

const (
	verbTimeout = 30 * time.Second
	paletteRows = 12
)

// openPalette shows the palette and asks the daemon for its verbs the
// first time; the deck commands are usable while they load.
func (m *Model) openPalette() tea.Cmd {
	m.mode = modePalette
	m.pal = &palette{}
	m.rebuildPalette()
	if m.verbs == nil && !m.verbsBusy {
		m.verbsBusy = true
		return m.loadVerbs()
	}
	return nil
}

func (m *Model) closePalette() {
	m.mode, m.pal = modeNormal, nil
}

// rebuildPalette lists the entries again: the deck commands, then every
// verb the daemon listed.
func (m *Model) rebuildPalette() {
	if m.pal == nil {
		return
	}
	items := m.deckCommands()
	for _, v := range m.verbs {
		v := v
		items = append(items, palItem{Cat: "tuios", Name: v.Verb, Hint: v.Description, Run: func() tea.Cmd { return m.openVerbForm(v) }})
	}
	m.pal.all = items
	m.pal.filter()
}

func (m *Model) loadVerbs() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
		defer cancel()
		var out struct {
			Verbs []verb `json:"verbs"`
		}
		if err := m.client.Call(ctx, "list-verbs", map[string]any{}, &out); err != nil {
			return verbsMsg{err: err}
		}
		if out.Verbs == nil {
			out.Verbs = []verb{}
		}
		return verbsMsg{verbs: out.Verbs}
	}
}

// matchRank says how well name answers the lower-cased query: a prefix
// first, then a word of the name starting with it, then the query inside
// the name, then its letters in order, then every word of the query found
// one of those ways. -1 is no match.
func matchRank(name, q string) int {
	switch {
	case strings.HasPrefix(name, q):
		return 0
	case wordPrefix(name, q):
		return 1
	case strings.Contains(name, q):
		return 2
	case subsequence(name, q):
		return 3
	}
	words := strings.Fields(q)
	if len(words) < 2 {
		return -1
	}
	for _, w := range words {
		if !strings.Contains(name, w) && !subsequence(name, w) {
			return -1
		}
	}
	return 4
}

func wordPrefix(name, q string) bool {
	for _, w := range strings.FieldsFunc(name, func(r rune) bool { return r == ' ' || r == '-' || r == '_' }) {
		if strings.HasPrefix(w, q) {
			return true
		}
	}
	return false
}

func subsequence(name, q string) bool {
	i := 0
	for _, r := range name {
		if i < len(q) && rune(q[i]) == r {
			i++
		}
	}
	return i == len(q)
}

// filter keeps the entries the query matches, best rank first and in their
// listed order within a rank, and keeps the highlight on an entry.
func (p *palette) filter() {
	q := strings.ToLower(strings.TrimSpace(p.Query))
	p.shown = p.shown[:0]
	if q == "" {
		p.shown = append(p.shown, p.all...)
	} else {
		type scored struct {
			rank int
			item palItem
		}
		var out []scored
		for _, it := range p.all {
			if r := matchRank(strings.ToLower(it.Name), q); r >= 0 {
				out = append(out, scored{r, it})
			}
		}
		sort.SliceStable(out, func(i, j int) bool { return out[i].rank < out[j].rank })
		for _, s := range out {
			p.shown = append(p.shown, s.item)
		}
	}
	if p.sel >= len(p.shown) {
		p.sel = len(p.shown) - 1
	}
	if p.sel < 0 {
		p.sel = 0
	}
}

func (p *palette) setQuery(q string) {
	p.Query = q
	p.sel = 0
	p.filter()
}

func (m *Model) handlePaletteKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	p := m.pal
	switch msg.String() {
	case "esc", "ctrl+c":
		m.closePalette()
	case "down", "ctrl+n":
		if p.sel+1 < len(p.shown) {
			p.sel++
		}
	case "up", "ctrl+p":
		if p.sel > 0 {
			p.sel--
		}
	case "enter":
		if p.sel < len(p.shown) {
			it := p.shown[p.sel]
			m.closePalette()
			return m, it.Run()
		}
	case "tab":
		if p.sel < len(p.shown) {
			sel := p.sel
			p.setQuery(p.shown[sel].Name)
		}
	case "backspace":
		if r := []rune(p.Query); len(r) > 0 {
			p.setQuery(string(r[:len(r)-1]))
		}
	case "ctrl+u":
		p.setQuery("")
	case "ctrl+w":
		q := strings.TrimRight(p.Query, " ")
		if i := strings.LastIndex(q, " "); i >= 0 {
			p.setQuery(q[:i+1])
		} else {
			p.setQuery("")
		}
	default:
		if k := msg.Key(); k.Text != "" && k.Mod == 0 {
			p.setQuery(p.Query + k.Text)
		}
	}
	return m, nil
}

// paletteLines draws the palette: a header, the query line and up to
// paletteRows entries, fitted into maxH lines of width.
func (m *Model) paletteLines(width, maxH int) []string {
	p := m.pal
	rows := paletteRows
	if rows > maxH-3 {
		rows = maxH - 3
	}
	if rows < 1 {
		rows = 1
	}
	title := fmt.Sprintf("─ command palette · %d/%d ", len(p.shown), len(p.all))
	fill := width - ansi.StringWidth(title)
	if fill < 0 {
		fill = 0
	}
	lines := []string{
		cBorder.Render(title + strings.Repeat("─", fill)),
		" " + cStatus.Render(">") + " " + p.Query + "▏",
	}
	if len(p.shown) == 0 {
		lines = append(lines, cDim.Render("   no matches"))
	}
	start := 0
	if p.sel >= rows {
		start = p.sel - rows + 1
	}
	end := start + rows
	if end > len(p.shown) {
		end = len(p.shown)
	}
	nameW := 0
	for _, it := range p.shown[start:end] {
		if w := ansi.StringWidth(it.Name); w > nameW {
			nameW = w
		}
	}
	if nameW > 32 {
		nameW = 32
	}
	for i := start; i < end; i++ {
		it := p.shown[i]
		tag := "[" + it.Cat + "]"
		room := width - 2 - 8 - nameW - 4
		hint := ""
		if room > 8 {
			hint = cDim.Render("— " + ansi.Truncate(panes.OneLine(it.Hint), room-2, "…"))
		}
		line := "  " + cDim.Render(pad(tag, 7)) + " " + pad(ansi.Truncate(it.Name, nameW, "…"), nameW) + "  " + hint
		if i == p.sel {
			line = cSel.Render(ansi.Strip(pad(line, width)))
		}
		lines = append(lines, line)
	}
	if m.verbsBusy {
		lines = append(lines, cDim.Render("   loading verbs…"))
	}
	return lines
}

// deckCommands is the deck's own side of the palette. Each one does what
// the key it stands for does.
func (m *Model) deckCommands() []palItem {
	needPane := func(run func(p *panes.Pane) tea.Cmd) func() tea.Cmd {
		return func() tea.Cmd {
			p := m.selected()
			if p == nil {
				m.setStatus("select a pane first")
				return nil
			}
			return run(p)
		}
	}
	items := []palItem{
		{"deck", "assign to task…", "assign the selected pane to a task", needPane(func(*panes.Pane) tea.Cmd { return m.openAssign() })},
		{"deck", "assign to project…", "assign the selected pane to a project", needPane(func(*panes.Pane) tea.Cmd { return m.openAssign() })},
		{"deck", "clear assignment", "drop the selected pane's task and project", needPane(func(p *panes.Pane) tea.Cmd { return m.assignPane(p.ID, p.Session, p.Name, "", "") })},
		{"deck", "new agent", "start an agent in a new pane (n)", m.openAgentForm},
		{"deck", "new terminal", "open a shell in a new pane (t)", m.openTerminalForm},
		{"deck", "prompt", "send a prompt or a line to the selected pane (p)", m.openPromptForm},
		{"deck", "prompt presets…", "create, edit and delete prompt presets: a prefix and a cwd for prompts and new panes", m.openPresetModal},
		{"deck", "queue prompt…", "keep a prompt for a task or a project, to start later from the queue", func() tea.Cmd { return m.openQueueForm(nil) }},
		{"deck", "queued prompts…", "choose, edit, delete and start the queued prompts", m.openQueueModal},
		{"deck", "capture", "write the selected pane's screen to the captures folder (c)", needPane(func(*panes.Pane) tea.Cmd { return m.captureToFile() })},
		{"deck", "close pane", "close the selected pane after a confirmation (x)", needPane(m.confirmClose)},
		{"deck", "refresh", "list the sessions and agents again (r)", m.refreshAll},
		{"deck", "settings…", "vim modes and key bindings, saved to the settings file", m.openSettingsForm},
		{"deck", "go to deck", "the panes page", func() tea.Cmd { m.page = pageDeck; return m.loadDetail(true) }},
		{"deck", "go to tasks", "the projects, tasks and subtasks page (T)", func() tea.Cmd { m.page = pageTasks; m.rebuildTasks(); return nil }},
		{"deck", "go to captures", "the captures folder page (C)", func() tea.Cmd { m.openCaptures(); return nil }},
		{"deck", "go to review queue", "completed prompts, oldest completion first (V)", m.openReviews},
		{"deck", "go to context", "the context items page (I)", func() tea.Cmd { m.openContext(); return nil }},
		{"deck", "filter context…", "filter the context page by kind, project, task or item text (f on the page)", func() tea.Cmd { m.openContext(); return m.openContextFilter() }},
		{"deck", "prototypes", "try the picker prototypes for assign and filter (U)", func() tea.Cmd { m.openProtos(); return nil }},
		{"deck", "context scope: current pane / all", "toggle the context page between the selected pane's items and all (t on the page)", func() tea.Cmd {
			m.page = pageContext
			if p := m.selected(); p != nil {
				m.cx.scopePane = ctxScopePane{p.Session, p.ID, p.Name}
			}
			m.toggleCtxScope()
			return nil
		}},
		{"deck", "extract context from pane", "find links, paths, prompts and commands in the selected pane (i)", needPane(func(*panes.Pane) tea.Cmd { return m.extractContext() })},
	}
	for _, g := range presets {
		items = append(items, palItem{"deck", "group by " + g.String(), "group the left rail of the deck by " + g.String() + " (o/O cycle)", func() tea.Cmd {
			m.page = pageDeck
			m.setGrouping(g)
			return nil
		}})
	}
	items = append(items, palItem{"deck", "group by…", "group the left rail by levels such as project > parent task", m.openGroupingForm})
	items = append(items, m.reminderCommands()...)
	return append(items,
		palItem{"deck", "new project", "add a project (N on the tasks page)", m.openProjectForm},
		palItem{"deck", "new task", "add a top-level task; the project of the selected pane is offered", m.openNewTaskForm},
		palItem{"deck", "new subtask of…", "add a subtask under a task", m.openSubtaskForm},
		palItem{"deck", "add many…", "add projects, tasks and subtasks from an indented outline (A)", func() tea.Cmd { return m.openMassAdd("") }},
		palItem{"deck", "add note", "add a note to the project or task selected on the tasks page", m.openAddNoteForm},
		palItem{"deck", "edit note…", "replace the text of a note of the selected project or task", m.openEditNoteForm},
		palItem{"deck", "delete note…", "delete a note of the selected project or task", m.openDeleteNoteForm},
		palItem{"deck", "delete capture", "delete the capture selected on the captures page (x)", m.deleteSelectedCapture},
	)
}

func (m *Model) refreshAll() tea.Cmd {
	if m.page == pageCaptures {
		m.loadCaptures()
	}
	if m.page == pageContext {
		m.rebuildContext()
	}
	m.rebuildTasks()
	return tea.Batch(m.refreshList(), m.loadDetail(true))
}

func (m *Model) deleteSelectedCapture() tea.Cmd {
	if m.page != pageCaptures {
		m.setStatus("open the captures page first (C)")
		return nil
	}
	if m.selectedCapture() == nil {
		m.setStatus("select a capture first")
		return nil
	}
	m.confirmDeleteCapture()
	return nil
}

// openNewTaskForm adds a task, starting at the top level of the selected
// pane's project.
func (m *Model) openNewTaskForm() tea.Cmd {
	parent := ""
	if p := m.selected(); p != nil {
		if pr, _, _ := m.store.Resolve(p.ID); pr != nil {
			parent = topLevel(pr.Name)
		}
	}
	return m.openNewTask(parent)
}

// openSubtaskForm adds a subtask, starting under the selected pane's task.
func (m *Model) openSubtaskForm() tea.Cmd {
	parent := ""
	if p := m.selected(); p != nil {
		if _, t, _ := m.store.Resolve(p.ID); t != nil {
			parent = m.taskPath(*t)
		}
	}
	return m.openNewTask(parent)
}

func (m *Model) sessionOptions() []string {
	var out []string
	for _, s := range m.merged {
		out = append(out, s.Name)
	}
	return out
}

// windowOptions is the ids and names of the panes of session, or of every
// session when it is empty.
func (m *Model) windowOptions(session string) []string {
	var out []string
	for _, s := range m.merged {
		if session != "" && s.Name != session {
			continue
		}
		for _, p := range s.Panes {
			out = append(out, p.Name, p.ID)
		}
	}
	return out
}

// loadNames fetches the option keys or the theme names for completion.
func (m *Model) loadNames(kind string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
		defer cancel()
		var names []string
		switch kind {
		case "options":
			var out struct {
				Options []struct {
					Path string `json:"path"`
				} `json:"options"`
			}
			if err := m.client.Call(ctx, "list-options", map[string]any{}, &out); err != nil {
				return namesMsg{kind: kind, err: err}
			}
			for _, o := range out.Options {
				names = append(names, o.Path)
			}
		case "themes":
			var out struct {
				Themes []string `json:"themes"`
			}
			if err := m.client.Call(ctx, "list-themes", map[string]any{}, &out); err != nil {
				return namesMsg{kind: kind, err: err}
			}
			names = out.Themes
		}
		return namesMsg{kind: kind, names: names}
	}
}

func paramHint(p verbParam) string {
	hint := p.Description
	if d := strings.Trim(string(p.Default), `"`); d != "" {
		hint += " (default " + d + ")"
	}
	return hint
}

// openVerbForm asks for a verb's params one at a time, required ones
// first, with the known values offered on tab, then calls it. A verb with
// no params is called at once.
func (m *Model) openVerbForm(v verb) tea.Cmd {
	params := append([]verbParam(nil), v.Params...)
	sort.SliceStable(params, func(i, j int) bool { return params[i].Required && !params[j].Required })
	if len(params) == 0 {
		return m.callVerb(v.Verb, map[string]any{})
	}
	f := &form{Title: v.Verb}
	sessionIdx, keyIdx := -1, -1
	for i, p := range params {
		switch p.Name {
		case "session":
			sessionIdx = i
		case "key":
			keyIdx = i
		}
	}
	isOption := v.Verb == "get-option" || v.Verb == "set-option"
	var cmds []tea.Cmd
	for _, p := range params {
		fl := field{Label: p.Name, Keep: true, Hint: paramHint(p)}
		if p.Required {
			fl.Label += "*"
		}
		switch {
		case len(p.Accepted) > 0:
			acc := p.Accepted
			fl.Options = func() []string { return acc }
		case p.Name == "session":
			fl.Options = m.sessionOptions
		case p.Name == "window":
			fl.Preview = func(opt string) previewSpec {
				session := ""
				if sessionIdx >= 0 {
					session = strings.TrimSpace(f.Fields[sessionIdx].Value)
				}
				return m.windowPreview(session)(opt)
			}
			fl.Options = func() []string {
				session := ""
				if sessionIdx >= 0 {
					session = strings.TrimSpace(f.Fields[sessionIdx].Value)
				}
				return m.windowOptions(session)
			}
		case p.Name == "key" && isOption:
			fl.Options = func() []string { return m.optionKeys }
			if m.optionKeys == nil {
				m.optionKeys = []string{}
				cmds = append(cmds, m.loadNames("options"))
			}
		case p.Name == "theme", p.Name == "value" && v.Verb == "set-option":
			fl.Options = func() []string {
				if keyIdx >= 0 && !strings.HasSuffix(strings.TrimSpace(f.Fields[keyIdx].Value), ".theme") {
					return nil
				}
				return m.themeNames
			}
			if m.themeNames == nil {
				m.themeNames = []string{}
				cmds = append(cmds, m.loadNames("themes"))
			}
		}
		f.Fields = append(f.Fields, fl)
	}
	f.Validate = func(vals []string) (int, string) {
		for i, p := range params {
			if vals[i] == "" {
				if p.Required {
					return i, p.Name + " is required"
				}
				continue
			}
			if _, err := coerceParam(p.Type, vals[i]); err != nil {
				return i, p.Name + ": " + err.Error()
			}
		}
		return 0, ""
	}
	f.Done = func(vals []string) tea.Cmd {
		args := map[string]any{}
		for i, p := range params {
			if vals[i] == "" {
				continue
			}
			val, _ := coerceParam(p.Type, vals[i])
			args[p.Name] = val
		}
		return m.callVerb(v.Verb, args)
	}
	m.mode, m.form = modeForm, f
	return tea.Batch(cmds...)
}

// coerceParam types what was typed by the param's declared type: ints and
// bools parsed, strings as they are, and for arrays, objects and any,
// JSON when it parses, else a comma-separated list for an array type.
func coerceParam(typ, s string) (any, error) {
	t := strings.ToLower(typ)
	switch t {
	case "int", "integer":
		n, err := strconv.Atoi(s)
		if err != nil {
			return nil, errors.New("not an integer")
		}
		return n, nil
	case "number", "float":
		n, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return nil, errors.New("not a number")
		}
		return n, nil
	case "bool", "boolean":
		switch strings.ToLower(s) {
		case "true", "on", "1", "yes":
			return true, nil
		case "false", "off", "0", "no":
			return false, nil
		}
		return nil, errors.New("not a boolean (true/false)")
	case "string", "":
		return s, nil
	}
	isArray := strings.Contains(t, "[]")
	var v any
	if json.Unmarshal([]byte(s), &v) == nil {
		if _, ok := v.([]any); ok || !isArray {
			return v, nil
		}
	}
	if !isArray {
		return s, nil
	}
	var parts []string
	for _, part := range strings.Split(s, ",") {
		if part = strings.TrimSpace(part); part != "" {
			parts = append(parts, part)
		}
	}
	if !strings.Contains(t, "int") {
		return parts, nil
	}
	ints := make([]int, 0, len(parts))
	for _, part := range parts {
		n, err := strconv.Atoi(part)
		if err != nil {
			return nil, errors.New("not a list of integers")
		}
		ints = append(ints, n)
	}
	return ints, nil
}

// callVerb runs one verb with a longer timeout than the deck's own calls,
// since a verb may wait on the daemon.
func (m *Model) callVerb(name string, args map[string]any) tea.Cmd {
	shown, _ := json.Marshal(args)
	m.setStatus("calling " + name + "…")
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), verbTimeout)
		defer cancel()
		var raw json.RawMessage
		err := m.client.Call(ctx, name, args, &raw)
		return resultMsg{verb: name, params: string(shown), raw: raw, err: err}
	}
}

// showResult puts a verb's result, pretty-printed, or its error in the
// detail pane of the deck page, where it stays until the selection or the
// tab changes.
func (m *Model) showResult(msg resultMsg) {
	m.page = pageDeck
	m.result = &verbResult{Verb: msg.verb, Params: msg.params}
	m.detailErr = ""
	m.scroll, m.follow, m.revision = 0, false, 0
	if msg.err != nil {
		m.detail = errorLines(msg.err)
		m.setStatus("error: " + msg.verb + ": " + msg.err.Error())
		return
	}
	raw := msg.raw
	if len(raw) == 0 {
		raw = json.RawMessage("null")
	}
	var buf bytes.Buffer
	if err := json.Indent(&buf, raw, "", "  "); err != nil {
		buf.Reset()
		buf.Write(raw)
	}
	m.detail = strings.Split(buf.String(), "\n")
	m.setStatus(msg.verb + ": ok")
}

// errorLines is a daemon error as the detail pane shows it: the code, the
// message and the hint, each in the error style.
func errorLines(err error) []string {
	var derr *daemon.Error
	if !errors.As(err, &derr) {
		return []string{cErr.Render("error: " + err.Error())}
	}
	lines := []string{cErr.Render("error " + derr.Code), cErr.Render(derr.Message)}
	if len(derr.Hint) > 0 {
		hint := string(derr.Hint)
		var s string
		var buf bytes.Buffer
		if json.Unmarshal(derr.Hint, &s) == nil {
			hint = s
		} else if json.Indent(&buf, derr.Hint, "", "  ") == nil {
			hint = buf.String()
		}
		lines = append(lines, "", cDim.Render("hint:"))
		for _, l := range strings.Split(hint, "\n") {
			lines = append(lines, cErr.Render(l))
		}
	}
	return lines
}

// resultLines draws the detail pane while it shows a verb's result.
func (m *Model) resultLines(width, height int) []string {
	r := m.result
	bar := cTabOn.Render(" result of " + r.Verb + " ")
	if room := width - ansi.StringWidth(bar) - 2; room > 4 {
		bar += "  " + cDim.Render(ansi.Truncate(r.Params, room, "…"))
	}
	lines := []string{bar}
	body := height - 1
	end := m.scroll + body
	if end > len(m.detail) {
		end = len(m.detail)
	}
	for _, l := range m.detail[m.scroll:end] {
		lines = append(lines, ansi.Truncate(l, width, ""))
	}
	for len(lines) < height {
		lines = append(lines, "")
	}
	if len(m.detail) > body {
		lines[height-1] = pad(lines[height-1], width-12) + cDim.Render(fmt.Sprintf("%4d/%d ↓G", m.scroll+1, len(m.detail)))
	}
	return lines
}

// openGroupingForm asks for a grouping as level names joined by ">"; tab
// completes the level being typed.
func (m *Model) openGroupingForm() tea.Cmd {
	m.mode = modeForm
	m.form = &form{
		Title: "group by",
		Fields: []field{{Label: "levels", Default: m.grouping.String(), Hint: "a > b > c, e.g. harness > project > parent task", Options: func() []string {
			head := ""
			if m.form != nil {
				v := m.form.Fields[0].Value
				if i := strings.LastIndex(v, ">"); i >= 0 {
					head = strings.TrimRight(v[:i], " ") + " > "
				}
			}
			var out []string
			if head == "" {
				out = append(out, lvTree.String())
			}
			for _, name := range levelNames() {
				out = append(out, head+name)
			}
			return out
		}}},
		Validate: func(v []string) (int, string) {
			if _, ok := parseGrouping(v[0]); !ok {
				return 0, "levels are " + strings.Join(levelNames(), ", ") + ", joined by >; or task tree"
			}
			return 0, ""
		},
		Done: func(v []string) tea.Cmd {
			g, _ := parseGrouping(v[0])
			m.page = pageDeck
			m.setGrouping(g)
			return nil
		},
	}
	return nil
}
