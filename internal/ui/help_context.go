package ui

import (
	"slices"
	"strings"

	"github.com/unixsuperhero/tuios-deck/internal/config"
)

var contextHelpModes = []string{"prompt", "saved prompts", "palette", "confirmation", "presets", "queue", "glyph table", "picker", "focus buffer", "detail", "turns"}

func (m *Model) helpContext() string {
	if m.focus != nil && !m.focus.clock.BufferUntil.IsZero() {
		return "focus buffer"
	}
	if m.live != nil {
		return config.ModeLive
	}
	switch m.mode {
	case modeForm:
		if m.form.recovery != nil {
			return "saved prompts"
		}
		if m.vimModes() && m.form.vimNormal {
			return config.ModeFormNormal
		}
		if m.form.Fields[m.form.Cur].Prompt {
			return "prompt"
		}
		if m.vimModes() {
			return config.ModeFormInsert
		}
		return "forms"
	case modePalette:
		return "palette"
	case modeConfirm:
		return "confirmation"
	case modePrompts:
		if m.pm.queue {
			return "queue"
		}
		return "presets"
	case modeGlyph:
		return "glyph table"
	case modeProto:
		return "picker"
	}
	if m.page == pageDeck && m.focusMain {
		if m.tab == tabTurns && m.result == nil {
			return "turns"
		}
		return "detail"
	}
	return m.page.String()
}

func (m *Model) contextHelpRows(mode string) []helpRow {
	switch mode {
	case "prompt":
		rows := []helpRow{
			{"ctrl+r", "browse, search and restore saved prompts", "prompt"},
			{"ctrl+u", "stash the full draft, then clear the current line", "prompt"},
			{"ctrl+z", "undo the last edit or restore", "prompt"},
			{"enter", "insert newline", "multiline prompt"},
			{"ctrl+s, ctrl+enter", "submit prompt", "prompt"},
			{"text, ?", "type into the prompt", "prompt"},
			{"backspace / ctrl+w", "delete previous character / word", "prompt"},
			{"shift+tab", "previous field", "prompt"},
			{"ctrl+n/p, up/down", "select completion", "open completion list"},
			{"tab / enter", "accept completion", "open completion list"},
			{"@", "complete a path", "prompt"},
			{"ctrl+c", "cancel the form", "prompt"},
		}
		if m.form != nil && !m.form.Fields[m.form.Cur].Multi {
			rows[3] = helpRow{"enter", "next field or submit", "prompt"}
		}
		if m.form != nil && m.form.Fields[m.form.Cur].Attach {
			rows = append(rows, helpRow{"ctrl+o", "choose attachment files", "prompt"}, helpRow{"/", "complete command or skill at start", "prompt"})
		}
		if m.vimModes() {
			key := (&config.Config{}).Binding(config.ModeFormInsert, "leave_insert")
			if m.settings != nil {
				key = m.settings.Binding(config.ModeFormInsert, "leave_insert")
			}
			rows = append(rows, helpRow{key, "close completion first; otherwise enter normal mode", "prompt"})
		} else {
			rows = append(rows, helpRow{"esc", "close completion first; otherwise cancel form", "prompt"})
		}
		return rows
	case "saved prompts":
		return []helpRow{
			{"text / paste", "search full prompt text; newest first", "saved prompts"},
			{"up/down, ctrl+p/n", "select a saved prompt", "saved prompts"},
			{"pgup/pgdown", "scroll the full text preview", "saved prompts"},
			{"enter", "restore without submitting; save the replaced draft", "saved prompts"},
			{"esc, ctrl+c", "return without changing the draft", "saved prompts"},
			{"backspace", "delete the last search character", "saved prompts"},
			{"ctrl+u", "clear search, not the draft", "saved prompts"},
		}
	case "palette", "confirmation", "presets", "queue", "glyph table", "focus buffer":
		var rows []helpRow
		for _, row := range m.helpRows("modals") {
			scope, _, _ := strings.Cut(row.scope, ";")
			if slices.Contains(strings.Split(scope, "/"), mode) {
				rows = append(rows, row)
			}
		}
		return rows
	case "picker":
		return m.pickerHelpRows()
	case "detail", "turns":
		var rows []helpRow
		for _, row := range m.helpRows(config.ModeDeck) {
			if row.scope == "deck rail" || mode == "detail" && row.scope == "Turns detail" {
				continue
			}
			rows = append(rows, row)
		}
		for i := range rows {
			name, _, _ := strings.Cut(rows[i].action, ":")
			switch name {
			case "down", "up":
				rows[i].action = name + ": scroll detail"
				if mode == "turns" {
					rows[i].action = name + ": select turn"
				}
			case "top":
				rows[i].action = "top: top of detail"
			case "bottom":
				rows[i].action = "bottom: follow newest output"
			}
		}
		if mode == "turns" {
			rows = append(rows, helpRow{"{/}", "jump turn group", "turns"})
		}
		return rows
	}
	return nil
}

func (m *Model) pickerHelpRows() []helpRow {
	rows := []helpRow{{"enter", "pick the highlighted target", "picker"}, {"esc", "clear filter or cancel", "picker"}, {"ctrl+c", "cancel", "picker"}}
	switch p := m.proto.(type) {
	case *treeView:
		rows = append(rows, helpRow{"text / paste", "filter names or item text", "picker"}, helpRow{"up/down, ctrl+p/n", "select result", "picker"}, helpRow{"backspace, ctrl+w/u", "edit filter", "picker"})
		if !p.typing && p.query == "" {
			rows = append(rows, helpRow{"j/k", "select result", "picker"}, helpRow{"h/l, left/right", "close/open branch", "picker"}, helpRow{"{/}", "jump sibling", "picker"}, helpRow{"(", "go to parent", "picker"})
		}
	case *hotkeyPicker:
		if p.more == nil {
			return append(rows, helpRow{"1–9", "pick target directly", "picker"}, helpRow{"0", "open all targets in fuzzy search", "picker"}, helpRow{"j/k, up/down", "select target", "picker"})
		}
		fallthroughRows := []helpRow{{"text / paste", "filter targets", "picker"}, {"ctrl+n/p, down/up", "select result", "picker"}, {"?", "toggle preview", "picker"}}
		rows = append(rows, fallthroughRows...)
	case *fuzzyPicker:
		rows = append(rows, helpRow{"text / paste", "filter targets", "picker"}, helpRow{"ctrl+n/p, down/up", "select result", "picker"}, helpRow{"?", "toggle preview", "picker"})
	case *treePicker:
		rows = append(rows, helpRow{"up/down, ctrl+p/n", "select target", "picker"}, helpRow{"?", "toggle preview", "picker"})
		if p.filtering {
			rows = append(rows, helpRow{"text / paste", "filter branches", "picker"})
		} else {
			rows = append(rows, helpRow{"j/k", "select target", "picker"}, helpRow{"h/l, left/right", "close/open branch", "picker"}, helpRow{"g/G, home/end", "first/last target", "picker"}, helpRow{"/", "start filtering", "picker"})
		}
	case *millerPicker:
		rows = append(rows, helpRow{"j/k, up/down", "select target in column", "picker"}, helpRow{"h/l, left/right", "previous/next column", "picker"}, helpRow{"?", "toggle preview", "picker"})
	case *chipsPicker:
		rows = append(rows, helpRow{"text / paste", "filter current segment", "picker"}, helpRow{"/ or tab", "lock a segment", "picker"}, helpRow{"backspace", "delete character or pop empty segment", "picker"}, helpRow{"ctrl+n/p, down/up", "select completion", "picker"}, helpRow{"?", "toggle preview", "picker"})
	}
	return rows
}
