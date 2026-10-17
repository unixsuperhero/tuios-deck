package ui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
)

// field is one line of a form: a label, the text typed so far, and a default
// used when the person leaves it empty.
type field struct {
	Label   string
	Value   string
	Default string
	Keep    bool // keep an empty value empty instead of taking the default
}

func (f field) result() string {
	if f.Value == "" && !f.Keep {
		return f.Default
	}
	return f.Value
}

// form is a sequence of fields filled one at a time on the status line.
type form struct {
	Title  string
	Fields []field
	Cur    int
	Done   func(values []string) tea.Cmd
}

// handleKey edits the current field. It reports whether the form finished,
// and the command the finish produced.
func (f *form) handleKey(msg tea.KeyPressMsg) (finished bool, cmd tea.Cmd) {
	cur := &f.Fields[f.Cur]
	switch msg.String() {
	case "enter":
		if f.Cur+1 < len(f.Fields) {
			f.Cur++
			return false, nil
		}
		values := make([]string, len(f.Fields))
		for i, fl := range f.Fields {
			values[i] = strings.TrimSpace(fl.result())
		}
		return true, f.Done(values)
	case "shift+tab", "up":
		if f.Cur > 0 {
			f.Cur--
		}
	case "backspace":
		if n := len(cur.Value); n > 0 {
			r := []rune(cur.Value)
			cur.Value = string(r[:len(r)-1])
		}
	case "ctrl+u":
		cur.Value = ""
	case "ctrl+w":
		cur.Value = strings.TrimRight(cur.Value, " ")
		if i := strings.LastIndex(cur.Value, " "); i >= 0 {
			cur.Value = cur.Value[:i+1]
		} else {
			cur.Value = ""
		}
	case "tab":
		if cur.Value == "" {
			cur.Value = cur.Default
		}
	default:
		if k := msg.Key(); k.Text != "" && k.Mod == 0 {
			cur.Value += k.Text
		}
	}
	return false, nil
}

func (f *form) paste(text string) {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\n", " ")
	f.Fields[f.Cur].Value += text
}
