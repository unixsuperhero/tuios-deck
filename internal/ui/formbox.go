package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/unixsuperhero/tuios-deck/internal/panes"
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
		body = append(body, m.currentField(fl, iw)...)
	}
	help := m.formHelp()
	lines := []string{boxTop(f.Title, width)}
	avail := maxH - 4
	if avail < 1 {
		avail = 1
	}
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
// with the cursor, the hint, and the completion list.
func (m *Model) currentField(fl field, iw int) []string {
	var out []string
	value := fl.Value
	if fl.Multi {
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
	hint := fl.Hint
	if fl.HintFn != nil {
		hint = fl.HintFn()
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
	return out
}

// formHelp names the keys the current field answers.
func (m *Model) formHelp() string {
	f := m.form
	cur := f.Fields[f.Cur]
	pos := fmt.Sprintf("%d/%d", f.Cur+1, len(f.Fields))
	switch {
	case cur.at > 0:
		return pos + " · ctrl+n/ctrl+p pick · tab or enter take the path, or open the folder · esc close the list"
	case len(f.list()) > 0:
		return pos + " · ctrl+n/ctrl+p pick · tab accept · enter accept or next · shift+tab back · esc cancel"
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
