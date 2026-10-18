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
}

type helpRow struct{ key, action, scope string }

func helpModes() []string {
	modes := append(config.Modes(), "pages", "forms", "modals", "actions")
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
	switch msg.String() {
	case "f1", "esc", "ctrl+c":
		m.help = nil
	case "tab", "right", "l":
		h.mode, h.scroll = (h.mode+1)%len(helpModes()), 0
	case "shift+tab", "left", "h":
		h.mode, h.scroll = (h.mode+len(helpModes())-1)%len(helpModes()), 0
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
			scope, _, _ := strings.Cut(row.scope, ";")
			if slices.Contains(strings.Split(scope, "/"), mode) {
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
		if mode != config.ModeDeck {
			scope = "forms, vim modes on"
		}
		rows = append(rows, helpRow{c.Binding(mode, a.Name), a.Name + ": " + a.Desc, scope})
	}
	switch mode {
	case config.ModeDeck:
		rows = append(rows, helpRow{"ctrl+c", "quit", "all pages, not dialogs"}, helpRow{"ctrl+p", "command palette", "all pages, not dialogs"},
			helpRow{"up/down", "previous/next pane or detail row", "deck page"}, helpRow{"left/right", "focus rail/detail", "deck page"},
			helpRow{"1/2/3/4", "Screen/Activity/Transcript/Turns", "deck page"}, helpRow{"[/]", "previous/next tab", "deck page"},
			helpRow{"home/end", "first pane/follow detail end", "deck page"}, helpRow{"pgup/pgdown, ctrl+u/d", "scroll detail half a page", "deck page"},
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
			{"tab/shift+tab, left/right", "choose mode", "help"}, {"u", "toggle actions without bindings", "help"}, {"j/k, up/down", "scroll", "help"}, {"pgup/pgdown, ctrl+u/d", "scroll page", "help"}, {"home/end, g/G", "first/last help row", "help"}, {"esc, ctrl+c", "close without changing selection", "help"},
			{"text, ?", "filter commands", "palette"}, {"up/down, ctrl+p/n", "select command", "palette"}, {"enter", "run command", "palette"}, {"tab", "complete command name", "palette"}, {"esc, ctrl+c", "close", "palette"},
			{"enter, y/Y", "confirm", "confirmation"}, {"esc, ctrl+c, n/N", "cancel", "confirmation"},
			{"j/k", "select row", "presets/queue"}, {"n/e/d", "new/edit/delete", "presets/queue"}, {"x", "add example preset", "presets"}, {"enter", "start queued prompt", "queue"}, {"esc, ctrl+c", "close", "presets/queue"},
			{"h/j/k/l, arrows", "move", "glyph table"}, {"text", "find or add glyph", "glyph table"}, {"enter", "pick glyph", "glyph table"},
			{"backspace", "clear typed or custom glyph", "glyph table"}, {"esc", "cancel", "glyph table"},
			{"ctrl+n/p, up/down", "select result", "typing pickers"}, {"enter", "pick result", "pickers"}, {"?", "toggle preview (not help)", "preview picker prototypes"}, {"esc", "clear filter or cancel", "pickers"}, {"ctrl+c", "cancel", "pickers/glyph table"},
		}
	case "actions":
		// Commands without a direct shortcut still remain available in the palette.
		bound := map[string]string{
			"go to review queue":       "review",
			"go to history":            "history",
			"keybindings and actions…": "help", "assign to task…": "assign", "assign to project…": "assign", "new agent": "new_agent", "new terminal": "new_terminal", "prompt": "prompt", "capture": "capture", "close pane": "close", "refresh": "refresh", "go to tasks": "tasks", "go to captures": "captures", "go to context": "context", "extract context from pane": "extract", "add many…": "add_many",
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

func (m *Model) helpView() string {
	w, h := m.helpSize()
	mode := helpModes()[m.help.mode]
	filter := "all actions"
	if m.help.unbound {
		filter = "unbound only"
	}
	lines := []string{cTitle.Render("Keybindings and actions"), "Mode: " + mode + " · " + filter}
	if mode == config.ModeFormNormal || mode == config.ModeFormInsert {
		lines = append(lines, fmt.Sprintf("vim_modes=%t; see forms for editing keys", m.vimModes()))
	} else {
		lines = append(lines, "TUI only. Configured keys override defaults; dialogs own their input.")
	}
	lines = append(lines, cHeader.Render("KEY                  ACTION / SCOPE"))
	var body []string
	note := ""
	switch mode {
	case config.ModeDeck, "pages", "actions":
		note = "Focus scopes the pane and task lists to a project or task and its descendants. All work tools remain available. New panes default to the focused scope; form targets remain editable. F unlocks."
	case "modals":
		note = "After 15 active minutes, the focus buffer pauses work. F1 opens its keybindings without pausing the 30-second unlock deadline."
	}
	if note != "" {
		body = append(body, strings.Split(ansi.Wrap(note, max(1, w-4), ""), "\n")...)
	}
	rows := m.helpRows(mode)
	for _, r := range rows {
		key := r.key
		if key == "" {
			key = "(unbound)"
		}
		line := fmt.Sprintf("%-20s %s [%s]", key, r.action, r.scope)
		body = append(body, strings.Split(ansi.Wrap(line, max(1, w-4), ""), "\n")...)
	}
	if len(rows) == 0 {
		body = append(body, "No actions without bindings in this mode. Choose actions for palette commands.")
	}
	body = append(body, "", "Help navigation")
	for _, r := range m.helpRows("modals") {
		if r.scope == "help" {
			body = append(body, strings.Split(ansi.Wrap(fmt.Sprintf("%-20s %s", r.key, r.action), max(1, w-4), ""), "\n")...)
		}
	}
	room := max(1, h-7)
	if h < 8 {
		lines = []string{"Help: " + mode}
		room = max(1, h-2)
	}
	m.help.scroll = min(m.help.scroll, max(0, len(body)-room))
	end := min(len(body), m.help.scroll+room)
	lines = append(lines, body[m.help.scroll:end]...)
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
