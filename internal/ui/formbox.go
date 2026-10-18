package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/unixsuperhero/tuios-deck/internal/panes"
	"github.com/unixsuperhero/tuios-deck/internal/store"
)

// formWidth is the width of the form box on a screen of width w.
func formWidth(w int) int {
	bw := w * 2 / 3
	if bw < 48 {
		bw = 48
	}
	if bw > 100 {
		bw = 100
	}
	if bw > w-2 {
		bw = w - 2
	}
	return bw
}

// formBox draws the form as a box width wide and at most maxH tall: the
// title in the top border, every field on its own line with the current
// one marked, the current field's lines, hint and completion list under it,
// and the keys at the bottom.
func (m *Model) formBox(width, maxH int) []string {
	f := m.form
	iw := width - 4
	if iw < 8 {
		iw = 8
	}
	avail := maxH - 4
	if avail < 1 {
		avail = 1
	}
	var body []string
	curStart := 0
	for i, fl := range f.Fields {
		if i != f.Cur {
			value := fl.Value
			if fl.Multi && strings.Contains(value, "\n") {
				value = panes.OneLine(value)
			}
			if value == "" && fl.Default != "" {
				value = cDim.Render(fl.Default)
			}
			body = append(body, ansi.Truncate("  "+fl.Label+": "+value, iw, "…"))
			continue
		}
		curStart = len(body)
		body = append(body, m.currentField(fl, iw, avail-(len(f.Fields)-1))...)
	}
	help := m.formHelp()
	lines := []string{boxTop(f.Title, width)}
	if len(body) > avail {
		start := curStart
		if start > len(body)-avail {
			start = len(body) - avail
		}
		body = body[start : start+avail]
	}
	for _, l := range body {
		lines = append(lines, boxRow(l, iw))
	}
	lines = append(lines,
		cBorder.Render("├"+strings.Repeat("─", width-2)+"┤"),
		boxRow(cDim.Render(ansi.Truncate(help, iw, "…")), iw),
		cBorder.Render("╰"+strings.Repeat("─", width-2)+"╯"),
	)
	return lines
}

// currentField is the lines of the field being edited: its label and value
// with the cursor, the hint, and the completion list. room is how many
// lines the box can give the current field; a preview takes what the list
// leaves, so the keys under the box stay in sight.
func (m *Model) currentField(fl field, iw, room int) []string {
	var out []string
	value := fl.Value
	hint := fl.Hint
	if fl.HintFn != nil {
		hint = fl.HintFn()
	}
	if fl.Multi && fl.Outline {
		out = append(out, cStatus.Render("› "+fl.Label+":"))
		out = append(out, m.outlineView(iw, room-1-strings.Count(hint, "\n")-1)...)
	} else if fl.Multi {
		out = append(out, cStatus.Render("› "+fl.Label+":"))
		vl := strings.Split(value, "\n")
		if len(vl) > listRows {
			out = append(out, cDim.Render(fmt.Sprintf("    ⋮ %d lines above", len(vl)-listRows)))
			vl = vl[len(vl)-listRows:]
		}
		for j, l := range vl {
			if j == len(vl)-1 {
				out = append(out, "    "+tail(l+"▏", iw-4))
			} else {
				out = append(out, "    "+ansi.Truncate(l, iw-4, "…"))
			}
		}
	} else {
		label := "› " + fl.Label + ": "
		if value == "" && fl.Default != "" {
			value = "▏" + cDim.Render(fl.Default+" (tab to take)")
		} else {
			value = tail(value+"▏", iw-ansi.StringWidth(label))
		}
		out = append(out, ansi.Truncate(cStatus.Render(label)+value, iw, "…"))
	}
	// A multiline field keeps its hint's lines, so the hint can show an
	// example of what to type.
	switch {
	case hint != "" && fl.Multi:
		for _, l := range strings.Split(hint, "\n") {
			out = append(out, cDim.Render(ansi.Truncate("    "+l, iw, "…")))
		}
	case hint != "":
		out = append(out, cDim.Render(ansi.Truncate("    "+panes.OneLine(hint), iw, "…")))
	}
	list := m.form.list()
	if len(list) == 0 {
		return out
	}
	hl := fl.hl - 1
	start := 0
	if hl >= listRows {
		start = hl - listRows + 1
	}
	end := start + listRows
	if end > len(list) {
		end = len(list)
	}
	for j := start; j < end; j++ {
		if j == hl {
			out = append(out, cSel.Render(pad("    ▸ "+ansi.Truncate(list[j], iw-6, "…"), iw)))
		} else {
			out = append(out, "      "+ansi.Truncate(list[j], iw-6, "…"))
		}
	}
	if len(list) > listRows {
		out = append(out, cDim.Render(fmt.Sprintf("      %d of %d", end-start, len(list))))
	}
	return append(out, m.previewBox(iw, room-len(out))...)
}

// outlineRows is the most lines of an Outline field the box shows at once.
const outlineRows = 14

// outlineView is the lines of the current Outline field, at most rows of
// them and never more than outlineRows, scrolled so the cursor's line is
// in sight, with the cursor drawn at its column.
func (m *Model) outlineView(iw, rows int) []string {
	fl := &m.form.Fields[m.form.Cur]
	rows = min(max(rows, 3), outlineRows)
	vl := strings.Split(fl.Value, "\n")
	line, col := fl.cursorLine()
	var known []string
	if fl.Known != nil {
		known = fl.Known(fl.Value)
	}
	// The window moves only when the cursor leaves it; a line marking what
	// is cut off above or below takes a row.
	if len(vl) <= rows {
		fl.top = 0
	} else {
		rows -= 2
		fl.top = min(max(fl.top, line-rows+1), line, len(vl)-rows)
	}
	end := min(fl.top+rows, len(vl))
	var out []string
	if fl.top > 0 {
		out = append(out, cDim.Render(fmt.Sprintf("    ⋮ %d lines above", fl.top)))
	}
	w := iw - 4
	for j := fl.top; j < end; j++ {
		l, mark := vl[j], ""
		if j < len(known) && store.IsFinal(known[j]) {
			mark = "  " + statusMark(known[j]) + " " + known[j]
		}
		var s string
		switch left := firstRunes(l, col); {
		case j != line:
			s = ansi.Truncate(l+mark, w, "…")
		case ansi.StringWidth(left)+1 > w:
			s = tail(left+"▏", w)
		default:
			s = ansi.Truncate(left+"▏"+l[len(left):]+mark, w, "…")
		}
		if j < len(known) && known[j] != "" {
			s = cDim.Render(s)
		}
		out = append(out, "    "+s)
	}
	if end < len(vl) {
		out = append(out, cDim.Render(fmt.Sprintf("    ⋮ %d lines below", len(vl)-end)))
	}
	return out
}

// formHelp names the keys the current field answers.
func (m *Model) formHelp() string { return m.vimFormHelp(m.formHelpPlain()) }

func (m *Model) formHelpPlain() string {
	f := m.form
	cur := f.Fields[f.Cur]
	pos := fmt.Sprintf("%d/%d · ctrl+z undo", f.Cur+1, len(f.Fields))
	switch {
	case cur.at > 0:
		return pos + " · ctrl+n/ctrl+p pick · tab or enter take the path, or open the folder · esc close the list"
	case cur.sl > 0:
		return pos + " · ctrl+n/ctrl+p pick · tab or enter take the command · esc close the list"
	case len(f.list()) > 0:
		return pos + " · ctrl+n/ctrl+p pick · tab accept · enter accept or next · shift+tab back · esc cancel"
	case cur.Multi && cur.Outline:
		return pos + " · ↑↓←→ move · enter split · tab/shift+tab indent · ctrl+u clear line · ctrl+s submit · esc cancel"
	case cur.Attach:
		return pos + " · enter newline · ctrl+s submit · @ attach a file · ctrl+o pick files · / command · shift+tab back · esc cancel"
	case cur.Multi:
		return pos + " · enter newline · ctrl+s submit · @ a file · shift+tab back · esc cancel"
	case f.Cur+1 < len(f.Fields):
		return pos + " · enter next · tab default · ctrl+s submit · shift+tab back · esc cancel"
	}
	return pos + " · enter submit · tab default · shift+tab back · esc cancel"
}

// tail keeps the end of s when it is wider than w, so the cursor stays in
// sight while typing a long value.
func tail(s string, w int) string {
	if w < 2 {
		w = 2
	}
	if n := ansi.StringWidth(s); n > w {
		return "…" + ansi.TruncateLeft(s, n-w+1, "")
	}
	return s
}

func boxTop(title string, width int) string {
	t := "╭─ " + title + " "
	if ansi.StringWidth(t) > width-2 {
		t = ansi.Truncate(t, width-2, "… ")
	}
	return cBorder.Render("╭─ ") + cTitle.Render(strings.TrimPrefix(t, "╭─ ")) +
		cBorder.Render(strings.Repeat("─", width-1-ansi.StringWidth(t))+"╮")
}

func boxRow(s string, iw int) string {
	return cBorder.Render("│") + " " + pad(s, iw) + " " + cBorder.Render("│")
}

// overlay lays box over base from column x, keeping base on either side.
func overlay(base, box string, x, w int) string {
	return ansi.Truncate(base, x, "") + "\x1b[m" + box + "\x1b[m" + ansi.TruncateLeft(base, x+w, "")
}
