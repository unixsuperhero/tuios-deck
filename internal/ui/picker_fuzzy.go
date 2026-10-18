package ui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// fuzzyPicker is prototypes 1 and 2: every target in one ranked list
// under (or, two-pane, over) a single query line. The two-pane take shows
// the highlighted target's preview on the right.
type fuzzyPicker struct {
	pickBase
	twoPane bool
	preview bool
	query   string
	hl      int
}

func (f *fuzzyPicker) open(m *Model, p purpose) {
	f.init(m, p)
	f.preview = f.twoPane
}

// items is the ranked list, with "+ add new…" at the end when assigning
// and a text match at the end when filtering by a typed query.
func (f *fuzzyPicker) items() []scored {
	list := f.m.fuzzyNodes(f.m.pickAll(f.purpose), f.query, f.m.nodePath)
	q := strings.TrimSpace(f.query)
	switch {
	case f.purpose == purposeAssign:
		list = append(list, scored{n: pnode{New: true, Text: q}, path: strings.TrimPrefix(addNew, "+ ")})
	case q != "":
		n := pnode{Kind: "any", Text: q}
		list = append(list, scored{n: n, path: `text "` + q + `"`})
	}
	return list
}

func (f *fuzzyPicker) current() *pnode {
	list := f.items()
	if f.hl < 0 || f.hl >= len(list) {
		return nil
	}
	return &list[f.hl].n
}

func (f *fuzzyPicker) previewing() *pnode {
	if !f.preview {
		return nil
	}
	return f.current()
}

func (f *fuzzyPicker) handleKey(msg tea.KeyPressMsg) tea.Cmd {
	list := f.items()
	key := msg.String()
	switch {
	case key == "esc":
		f.finish(nil)
	case key == "enter":
		if f.hl >= 0 && f.hl < len(list) {
			n := list[f.hl].n
			f.finish(&n)
		}
	case moveSel(&f.hl, len(list), key):
	case editText(&f.query, msg):
		f.hl = 0
	}
	return nil
}

func (f *fuzzyPicker) view(width, height int) []string {
	list := f.items()
	iw := width - 4
	bodyH := height - 4
	if bodyH < 3 {
		bodyH = 3
	}
	total := len(f.m.pickAll(f.purpose))
	count := cDim.Render(fmt.Sprintf("%d/%d", len(list)-1, total))
	if f.purpose == purposeFilter && strings.TrimSpace(f.query) == "" {
		count = cDim.Render(fmt.Sprintf("%d/%d", len(list), total))
	}
	query := " " + cStatus.Render(">") + " " + f.query + "▏"
	query = pad(query, iw-ansi.StringWidth(count)) + count

	listW := iw
	if f.preview {
		listW = iw/2 - 1
	}
	rows := bodyH - 2
	var listLines []string
	start := window(f.hl, rows, len(list))
	for i := start; i < len(list) && len(listLines) < rows; i++ {
		listLines = append(listLines, nodeRow(list[i], listW, i == f.hl))
	}
	if len(list) == 0 {
		listLines = append(listLines, cDim.Render("   no matches"))
	}
	for len(listLines) < rows {
		listLines = append(listLines, "")
	}
	if f.preview {
		pv := f.m.previewLines(f.current(), iw-listW-2, rows)
		for i := range listLines {
			listLines[i] = pad(listLines[i], listW) + cBorder.Render(" │") + pv[i]
		}
	}
	rule := cBorder.Render(strings.Repeat("─", iw))
	var body []string
	if f.twoPane {
		body = append([]string{query, rule}, listLines...)
	} else {
		body = append(listLines, rule, query)
	}
	help := "type to filter · ctrl+n/ctrl+p move · enter pick · esc cancel"
	return pickerBox(f.m.protoRun.title, body, help, width)
}
