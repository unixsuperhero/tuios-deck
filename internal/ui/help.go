package ui

import (
	"fmt"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/unixsuperhero/tuios-deck/internal/config"
)

const keyHelpHint = "F1 keybindings"

type keyHelp struct {
	mode, scroll int
	unbound      bool
	// query filters every mode's rows at once; typing is true while the
	// query line takes the keys.
	query  string
	typing bool
}

type helpRow struct{ key, action, scope string }

func helpModes() []string {
	// The live mode's tab comes after the fixed ones, so the order of the
	// first tabs stays what it was.
	modes := []string{config.ModeDeck, config.ModeFormNormal, config.ModeFormInsert, "pages", "forms", "modals", "actions", config.ModeLive}
	for p := pageTasks; p <= pageHistory; p++ {
		modes = append(modes, p.String())
	}
	return append(modes, contextHelpModes...)
}

func (m *Model) openHelp() tea.Cmd {
	mode := m.helpContext()
	m.help = &keyHelp{mode: max(0, slices.Index(helpModes(), mode))}
	if m.verbs == nil && !m.verbsBusy {
		m.verbsBusy = true
		return m.loadVerbs()
	}
	return nil
}

func (m *Model) handleHelpKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	h := m.help
	if h.typing {
		switch msg.String() {
		case "f1", "ctrl+c":
			m.help = nil
		case "esc":
			h.query, h.typing, h.scroll = "", false, 0
		case "enter":
			h.typing = false
		case "backspace":
			if h.query == "" {
				h.typing = false
			} else {
				r := []rune(h.query)
				h.query, h.scroll = string(r[:len(r)-1]), 0
			}
		case "ctrl+u":
			h.query, h.scroll = "", 0
		case "up", "down", "pgup", "pgdown", "ctrl+d", "home", "end":
			h.typing = false
			m.handleHelpKey(msg)
			h.typing = true
		default:
			if k := msg.Key(); k.Text != "" && k.Mod&^tea.ModShift == 0 {
				h.query, h.scroll = h.query+k.Text, 0
			}
		}
		return m, nil
	}
	switch msg.String() {
	case "f1", "ctrl+c":
		m.help = nil
	case "esc":
		if h.query != "" {
			h.query, h.scroll = "", 0
		} else {
			m.help = nil
		}
	case "/":
		h.typing = true
	case "tab", "right", "l":
		h.mode, h.scroll, h.query = (h.mode+1)%len(helpModes()), 0, ""
	case "shift+tab", "left", "h":
		h.mode, h.scroll, h.query = (h.mode+len(helpModes())-1)%len(helpModes()), 0, ""
	case "u":
		h.unbound, h.scroll = !h.unbound, 0
	case "j", "down":
		h.scroll++
	case "k", "up":
		h.scroll = max(0, h.scroll-1)
	case "pgdown", "ctrl+d":
		_, height := m.helpSize()
		h.scroll += max(1, height-7)
	case "pgup", "ctrl+u":
		_, height := m.helpSize()
		h.scroll = max(0, h.scroll-max(1, height-7))
	case "g", "home":
		h.scroll = 0
	case "G", "end":
		h.scroll = int(^uint(0) >> 1)
	}
	return m, nil
}

func (m *Model) helpRows(mode string) []helpRow {
	if slices.Contains(contextHelpModes, mode) {
		if m.help.unbound {
			return nil
		}
		return m.contextHelpRows(mode)
	}
	c := m.settings
	if c == nil {
		c = &config.Config{}
	}
	var rows []helpRow
	for p := pageTasks; p <= pageHistory; p++ {
		if mode != p.String() {
			continue
		}
		for _, row := range m.helpRows("pages") {
			scope, rest, more := strings.Cut(row.scope, ";")
			if slices.Contains(strings.Split(scope, "/"), mode) {
				row.scope = mode + " page"
				if more {
					row.scope += ";" + rest
				}
				rows = append(rows, row)
			}
		}
		for _, row := range m.helpRows(config.ModeDeck) {
			if strings.HasPrefix(row.scope, "all pages") {
				rows = append(rows, row)
			}
		}
		return rows
	}
	for _, a := range config.ActionsOf(mode) {
		scope := "deck page"
		if a.Global {
			scope = "all pages, not dialogs"
		}
		switch {
		case mode == config.ModeLive && a.Name == "leader":
			scope = "live"
		case mode == config.ModeLive:
			scope = "live, after the prefix"
		case mode != config.ModeDeck:
			scope = "forms, vim modes on"
		case deckMotion[a.Name]:
			scope = deckMotionScope
		}
		rows = append(rows, helpRow{c.Binding(mode, a.Name), a.Name + ": " + a.Desc, scope})
	}
	switch mode {
	case config.ModeLive:
		leader := c.Binding(config.ModeLive, "leader")
		rows = append(rows, helpRow{leader + " " + leader, "type the prefix key itself into the pane", "live"},
			helpRow{"esc", "cancel the prefix", "live, after the prefix"},
			helpRow{"any other key", "typed into the pane, encoded for its modes by the daemon", "live"},
			helpRow{"paste", "pasted into the pane, bracketed when its program asked", "live"},
			helpRow{"F1", "goes to the pane too; " + leader + " " + c.Binding(config.ModeLive, "help") + " opens this", "live"})
	case config.ModeDeck:
		rows = append(rows, helpRow{"ctrl+c", "quit, after a confirmation", "all pages, not dialogs"}, helpRow{"ctrl+p", "command palette", "all pages, not dialogs"},
			helpRow{"up/down", "previous/next pane or detail row", deckMotionScope}, helpRow{"left/right", "focus rail/detail", deckMotionScope},
			helpRow{"1/2/3/4", "Screen/Activity/Transcript/Turns", deckMotionScope}, helpRow{"[/]", "previous/next tab", deckMotionScope},
			helpRow{"home/end", "first pane/follow detail end", deckMotionScope}, helpRow{"pgup/pgdown, ctrl+u/d", "scroll detail half a page", deckMotionScope},
			helpRow{"enter", "refresh", "deck rail"}, helpRow{"{/}, (/)", "jump group/level", "deck rail"},
			helpRow{"enter/space", "expand selected turn", "Turns detail"}, helpRow{"y", "copy selected turn", "Turns detail"},
			helpRow{"U", "picker prototypes", "all pages, not dialogs"})
	case config.ModeFormInsert:
		rows = append(rows, m.helpRows("forms")...)
	case config.ModeFormNormal:
		rows = append(rows, helpRow{"up/down", "previous/next field", "normal mode"}, helpRow{"ctrl+s", "submit", "normal mode"}, helpRow{"ctrl+c", "cancel", "normal mode"}, helpRow{"ctrl+z", "undo field edit", "normal mode"}, helpRow{"ctrl+r", "browse saved prompts", "prompt field"})
	case "pages":
		rows = []helpRow{
			{c.Binding(config.ModeDeck, "reset_view"), "reset filters/sort; no deletion or focus unlock", "context/tasks/review; pages without view controls unchanged"},
			{"esc", "return to deck", "tasks/captures/context/prototypes/history/review"},
			{"j/k, up/down", "next/previous row", "tasks/captures/context/prototypes/history/review"},
			{"g/G, home/end", "first/last row", "tasks/context/history"},
			{"{/}, (/)", "jump group/level", "tasks"}, {"N", "new project", "tasks"}, {"n", "new task/subtask", "tasks"},
			{"e", "edit", "tasks"}, {"s", "cycle task status", "tasks"}, {"m/M", "colour/glyph", "tasks"}, {"a", "assign selected pane", "tasks"},
			{"E", "toggle saved easy-task mark", "tasks; task selected"},
			{"z", "snooze until midnight / unsnooze", "tasks; task selected"},
			{"enter", "go to assigned pane", "tasks/captures"}, {"x", "delete selected item", "tasks/captures/context"},
			{"J/K", "scroll capture", "captures"}, {"G", "follow capture end", "captures"},
			{"{/}", "jump group", "context"}, {"t", "current pane/all scope", "context"}, {"s", "cycle sort", "context"},
			{"f", "filter", "context"}, {"o/O", "cycle grouping", "context"}, {"*", "star", "context"}, {"!/P", "pin", "context"},
			{"a", "assign item", "context"}, {"y", "copy item", "context"}, {"enter", "insert item", "context"}, {"ctrl+o", "open item", "context"}, {"w", "open with", "context"},
			{"r", "refresh", "tasks/captures/context"}, {"enter", "try selected picker", "prototypes"},
			{"enter", "go to source pane", "history"}, {"r", "refresh", "history"},
			{"J/K, pgdown/pgup, ctrl+d/u", "scroll entry detail half a page", "history"},
			{"enter", "open selected prompt and mark reviewed", "review"},
			{"tab/shift+tab", "switch awaiting review / reviewed history", "review"},
			{"J/K, pgdown/pgup, ctrl+d/u", "scroll opened prompt half a page", "review"},
			{"r", "refresh completed prompts", "review"},
		}
	case "forms":
		rows = []helpRow{
			{"text, ?", "type in the current field", "plain forms / vim insert"},
			{"enter", "next field; submit on last field", "single-line, no completion selected"},
			{"enter", "newline", "multiline"}, {"ctrl+s, ctrl+enter", "submit", "plain forms / vim insert"},
			{"shift+tab, up", "previous field", "no completion list; not outline"}, {"tab", "accept completion or empty field's default", "plain forms / vim insert"},
			{"up/down, ctrl+p/n", "previous/next completion", "completion list"}, {"enter", "accept highlighted completion", "text completion list"},
			{"esc", "close completion first; otherwise cancel", "plain forms"}, {"ctrl+c", "cancel", "forms"},
			{"backspace", "delete previous character", "plain forms / vim insert"}, {"ctrl+u", "clear current line, keeping earlier lines", "non-outline"}, {"ctrl+w", "delete previous word", "plain forms / vim insert"},
			{"ctrl+z", "undo the current field's last edit or paste", "plain forms / vim insert"},
			{"ctrl+u", "save the full prompt before clearing its current line", "prompt fields"},
			{"ctrl+r", "browse, search and restore saved prompts", "prompt fields, including vim normal"},
			{"up/down, ctrl+p/n", "select stash; PgUp/PgDn scroll its preview", "saved prompts"},
			{"enter / esc", "restore (saving replaced draft) / return unchanged", "saved prompts"},
			{"@", "complete path", "multiline, not outline"}, {"/", "complete command at start", "prompt fields"}, {"ctrl+o", "pick attachments", "attachment fields"},
			{"arrows", "move cursor", "outline"}, {"tab/shift+tab", "indent/outdent", "outline"}, {"ctrl+u", "clear line", "outline"},
			{"ctrl+h/l", "walk parent up/down", "task form"},
		}
	case "modals":
		rows = []helpRow{
			{"j/k, up/down, ctrl+n/p", "select project or task", "focus tree"},
			{"l/right, h/left", "open branch/enter child; close branch/go to parent", "focus tree"},
			{"g/G, home/end", "first/last visible row", "focus tree"},
			{"/", "filter tree, keeping matching ancestors", "focus tree"},
			{"enter", "focus selected project/task and descendants", "focus tree"},
			{"?", "toggle preview", "focus tree"},
			{"esc, ctrl+c", "clear filter or cancel; ctrl+c always cancels", "focus tree"},
			{"enter", "continue focus for a fresh 15-active-minute cycle", "focus buffer; fixed key"},
			{"F/esc", "unlock focus now", "focus buffer; fixed keys, not configured bindings"},
			{"q/ctrl+c", "quit the deck", "focus buffer; fixed keys"},
			{"tab/shift+tab, left/right", "choose mode", "help"}, {"/", "search every mode; enter keeps the filter, esc clears it", "help"}, {"u", "toggle actions without bindings", "help"}, {"j/k, up/down", "scroll", "help"}, {"pgup/pgdown, ctrl+u/d", "scroll page", "help"}, {"home/end, g/G", "first/last help row", "help"}, {"esc, ctrl+c", "close without changing selection", "help"},
			{"text, ?", "filter commands", "palette"}, {"up/down, ctrl+p/n", "select command", "palette"}, {"enter", "run command", "palette"}, {"tab", "complete command name", "palette"}, {"esc, ctrl+c", "close", "palette"},
			{"enter, y/Y", "confirm", "confirmation"}, {"esc, ctrl+c, n/N", "cancel", "confirmation"},
			{"j/k", "select row", "presets/queue"}, {"n/e/d", "new/edit/delete", "presets/queue"}, {"x", "add example preset", "presets"}, {"enter", "start queued prompt", "queue"}, {"esc, ctrl+c", "close", "presets/queue"},
			{"h/j/k/l, arrows", "move", "glyph table"}, {"text", "find or add glyph", "glyph table"}, {"enter", "pick glyph", "glyph table"},
			{"backspace", "clear typed or custom glyph", "glyph table"}, {"esc", "cancel", "glyph table"},
			{"j/k, up/down", "select node", "task tree"}, {"g/G", "project/last node", "task tree"}, {"s", "cycle status: open, done, skipped, planned", "task tree"},
			{"e", "edit node; the tree comes back", "task tree"}, {"n", "add a subtask under the node in the outline", "task tree"},
			{"x", "delete node, naming what goes with it", "task tree"}, {"a", "assign the pane to the node (unassigned: the assign picker)", "task tree"},
			{"enter", "tasks page with the node selected", "task tree"}, {"esc, q", "close; rail selection and detail unchanged", "task tree"},
			{"ctrl+n/p, up/down", "select result", "typing pickers"}, {"enter", "pick result", "pickers"}, {"?", "toggle preview (not help)", "preview picker prototypes"}, {"esc", "clear filter or cancel", "pickers"}, {"ctrl+c", "cancel", "pickers/glyph table"},
		}
	case "actions":
		// Commands without a direct shortcut still remain available in the palette.
		bound := map[string]string{
			"go to review queue":       "review",
			"go to history":            "history",
			"keybindings and actions…": "help", "live": "live", "assign to task…": "assign", "assign to project…": "assign", "task tree…": "tree", "new agent": "new_agent", "new terminal": "new_terminal", "prompt": "prompt", "capture": "capture", "close pane": "close", "refresh": "refresh", "go to tasks": "tasks", "go to captures": "captures", "go to context": "context", "extract context from pane": "extract", "add many…": "add_many",
		}
		fixed := map[string]helpRow{
			"prototypes": {"U", "", "all pages"}, "go to deck": {"esc", "", "other pages"}, "new project": {"N", "", "tasks"}, "filter context…": {"f", "", "context"}, "context scope: current pane / all": {"t", "", "context"}, "delete capture": {"x", "", "captures"},
		}
		for _, cmd := range m.deckCommands() {
			r := helpRow{"", cmd.Name, "palette only"}
			if action := bound[cmd.Name]; action != "" {
				r.key, r.scope = c.Binding(config.ModeDeck, action), "deck page; also palette"
				for _, a := range config.ActionsOf(config.ModeDeck) {
					if a.Name == action && a.Global {
						r.scope = "all pages; also palette"
					}
				}
			} else if f, ok := fixed[cmd.Name]; ok {
				r.key, r.scope = f.key, f.scope+"; also palette"
			}
			rows = append(rows, r)
		}
		for _, v := range m.verbs {
			rows = append(rows, helpRow{"", v.Verb, "daemon command via palette"})
		}
	}
	if m.help.unbound {
		out := rows[:0]
		for _, r := range rows {
			if r.key == "" {
				out = append(out, r)
			}
		}
		rows = out
	}
	return rows
}

func (m *Model) helpSize() (int, int) {
	w, h := max(1, m.width), max(1, m.height)
	return w - 2*min(5, max(0, (w-32)/2)), h - 2*min(5, max(0, (h-8)/2))
}

// helpGroup is one header in the help body and the rows under it.
type helpGroup struct {
	title string
	rows  []helpRow
}

// deckMotionScope groups the deck page's movement keys under one header.
const deckMotionScope = "deck page, motion"

var deckMotion = map[string]bool{"left": true, "right": true, "down": true, "up": true, "top": true, "bottom": true,
	"scroll_down": true, "scroll_up": true, "next_tab": true, "prev_tab": true, "group_next": true, "group_prev": true}

// helpGroups groups a tab's rows by scope: the part before ";" is the
// header and the rest stays on the row. The pages tab gets one header per
// page instead, in page order, so a page's keys read together. A page's
// own tab lists that page's keys first; otherwise scopes for every page
// come first, daemon verbs last, the rest alphabetically.
func helpGroups(mode string, rows []helpRow) []helpGroup {
	var groups []helpGroup
	add := func(title string, r helpRow) {
		for i := range groups {
			if groups[i].title == title {
				groups[i].rows = append(groups[i].rows, r)
				return
			}
		}
		groups = append(groups, helpGroup{title, []helpRow{r}})
	}
	if mode == "pages" {
		for p := pageTasks; p <= pageHistory; p++ {
			for _, r := range rows {
				scope, rest, _ := strings.Cut(r.scope, ";")
				if slices.Contains(strings.Split(scope, "/"), p.String()) {
					r.scope = strings.TrimSpace(rest)
					add(p.String()+" page", r)
				}
			}
		}
		return groups
	}
	for _, r := range rows {
		scope, rest, _ := strings.Cut(r.scope, ";")
		r.scope = strings.TrimSpace(rest)
		add(strings.TrimSpace(scope), r)
	}
	rank := func(t string) int {
		switch {
		case pageTabs[mode] && t == mode+" page":
			return -1
		case t == "all pages, not dialogs":
			return 0
		case strings.HasPrefix(t, "all pages"):
			return 1
		case strings.HasPrefix(t, "daemon command"):
			return 3
		}
		return 2
	}
	slices.SortStableFunc(groups, func(a, b helpGroup) int {
		if d := rank(a.title) - rank(b.title); d != 0 {
			return d
		}
		return strings.Compare(a.title, b.title)
	})
	return groups
}

// helpSearch filters every mode's rows by the query: case-insensitive
// substrings, every word of the query somewhere in the key, action or scope.
// A row already listed under an earlier mode is not repeated, and the page
// tabs are left out because the pages tab holds all of their rows.
var pageTabs = func() map[string]bool {
	tabs := map[string]bool{}
	for p := pageTasks; p <= pageHistory; p++ {
		tabs[p.String()] = true
	}
	return tabs
}()

func (m *Model) helpSearch(query string) []helpGroup {
	words := strings.Fields(strings.ToLower(query))
	seen := map[helpRow]bool{}
	var groups []helpGroup
	for _, mode := range helpModes() {
		if pageTabs[mode] {
			continue
		}
		g := helpGroup{title: mode}
		for _, r := range m.helpRows(mode) {
			text := strings.ToLower(r.key + " " + r.action + " " + r.scope)
			if seen[r] || slices.ContainsFunc(words, func(w string) bool { return !strings.Contains(text, w) }) {
				continue
			}
			seen[r] = true
			g.rows = append(g.rows, r)
		}
		if len(g.rows) > 0 {
			groups = append(groups, g)
		}
	}
	return groups
}

// helpTabs draws the tab bar in width cells: every tab when they fit,
// otherwise the current one and as many neighbours as fit, with an
// ellipsis for the rest.
func helpTabs(modes []string, cur, width int) string {
	draw := func(lo, hi int) (string, int) {
		var parts []string
		if lo > 0 {
			parts = append(parts, "…")
		}
		for i := lo; i <= hi; i++ {
			name := " " + modes[i] + " "
			if i == cur {
				name = cSel.Render(name)
			}
			parts = append(parts, name)
		}
		if hi < len(modes)-1 {
			parts = append(parts, "…")
		}
		out := strings.Join(parts, "│")
		return out, ansi.StringWidth(out)
	}
	lo, hi := cur, cur
	for grew := true; grew; {
		grew = false
		if hi < len(modes)-1 {
			if _, w := draw(lo, hi+1); w <= width {
				hi, grew = hi+1, true
			}
		}
		if lo > 0 {
			if _, w := draw(lo-1, hi); w <= width {
				lo, grew = lo-1, true
			}
		}
	}
	out, _ := draw(lo, hi)
	return ansi.Truncate(out, width, "…")
}

func (m *Model) helpView() string {
	w, h := m.helpSize()
	modes := helpModes()
	mode := modes[m.help.mode]
	filter := "all actions"
	if m.help.unbound {
		filter = "unbound only"
	}
	searching := m.help.typing || m.help.query != ""
	lines := []string{cTitle.Render("Keybindings and actions")}
	if searching {
		lines = append(lines, "Search: every mode · "+filter)
	} else {
		lines = append(lines, "Mode: "+mode+" · "+filter)
		if h >= 14 {
			lines = append(lines, helpTabs(modes, m.help.mode, max(1, w-4)))
		}
	}
	if searching {
		lines = append(lines, "Rows from every mode whose key, action or scope hold each word.")
	} else if mode == config.ModeFormNormal || mode == config.ModeFormInsert {
		lines = append(lines, fmt.Sprintf("vim_modes=%t; see forms for editing keys", m.vimModes()))
	} else {
		lines = append(lines, "TUI only. Configured keys override defaults; dialogs own their input.")
	}
	lines = append(lines, cHeader.Render("KEY                  ACTION / SCOPE"))
	var body []string
	wrap := func(s string) { body = append(body, strings.Split(ansi.Wrap(s, max(1, w-4), ""), "\n")...) }
	note := ""
	switch mode {
	case config.ModeDeck, "pages", "actions":
		note = "Focus scopes the pane and task lists to a project or task and its descendants. All work tools remain available. New panes default to the focused scope; form targets remain editable. F unlocks."
	case "modals":
		note = "After 15 active minutes, the focus buffer pauses work. F1 opens its keybindings without pausing the 30-second unlock deadline."
	}
	if note != "" && !searching {
		wrap(note)
	}
	var groups []helpGroup
	if searching {
		groups = m.helpSearch(m.help.query)
	} else {
		groups = helpGroups(mode, m.helpRows(mode))
	}
	for _, g := range groups {
		body = append(body, cHeader.Render("── "+g.title))
		for _, r := range g.rows {
			key := r.key
			if key == "" {
				key = "(unbound)"
			}
			line := fmt.Sprintf("%-20s %s", key, r.action)
			if r.scope != "" {
				line += " [" + r.scope + "]"
			}
			wrap(line)
		}
	}
	switch {
	case len(groups) == 0 && searching:
		body = append(body, "Nothing matches. backspace edits the search; esc clears it.")
	case len(groups) == 0:
		body = append(body, "No actions without bindings in this mode. Choose actions for palette commands.")
	}
	body = append(body, "", "Help navigation")
	for _, r := range m.helpRows("modals") {
		if r.scope == "help" {
			wrap(fmt.Sprintf("%-20s %s", r.key, r.action))
		}
	}
	var footer []string
	if searching {
		q := "/" + m.help.query
		if m.help.typing {
			q += "▏ enter keep · esc clear"
		} else {
			q += " · / edit · esc clear"
		}
		footer = append(footer, q)
	}
	room := max(1, h-3-len(lines)-len(footer))
	if h < 8 {
		lines = []string{"Help: " + mode}
		room = max(1, h-2)
	}
	m.help.scroll = min(m.help.scroll, max(0, len(body)-room))
	end := min(len(body), m.help.scroll+room)
	lines = append(lines, body[m.help.scroll:end]...)
	if h >= 8 {
		lines = append(lines, footer...)
	}
	lines = append(lines, cDim.Render(fmt.Sprintf("F1 close · %d-%d/%d", m.help.scroll+1, end, len(body))))
	if w >= 8 && h >= 8 {
		box := []string{boxTop("help", w)}
		for _, l := range lines {
			box = append(box, boxRow(l, w-4))
		}
		box = append(box, cBorder.Render("╰"+strings.Repeat("─", w-2)+"╯"))
		lines = box
	}
	if len(lines) > h {
		lines = lines[:h]
	}
	for i := range lines {
		lines[i] = pad(lines[i], w)
	}
	return lipgloss.Place(max(1, m.width), max(1, m.height), lipgloss.Center, lipgloss.Center, strings.Join(lines, "\n"))
}
