package ui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
)

// field is one line of a form: a label, the text typed so far, and a default
// used when the person leaves it empty. A field with Options completes on
// tab: the typed prefix to the unique match, or round the matches on
// repeated tabs. Hint, when set, is shown on the help line.
type field struct {
	Label   string
	Value   string
	Default string
	Keep    bool // keep an empty value empty instead of taking the default
	Hint    string
	Options func() []string

	matches []string // the completion cycle in progress, nil when none
	match   int
}

func (f field) result() string {
	if f.Value == "" && !f.Keep {
		return f.Default
	}
	return f.Value
}

// completions is every option the typed value is a prefix of, ignoring
// case. An option is a path like "project / task / subtask", so a prefix
// of its last segment matches too.
func (f field) completions() []string {
	if f.Options == nil {
		return nil
	}
	prefix := strings.ToLower(strings.TrimSpace(f.Value))
	var out []string
	for _, opt := range f.Options() {
		lower := strings.ToLower(opt)
		last := lower
		if i := strings.LastIndex(lower, " / "); i >= 0 {
			last = lower[i+3:]
		}
		if strings.HasPrefix(lower, prefix) || strings.HasPrefix(last, prefix) {
			out = append(out, opt)
		}
	}
	return out
}

// complete is what tab does on the field: an empty value takes the default,
// else the value is completed from the options.
func (f *field) complete() {
	if f.matches != nil && f.Value == f.matches[f.match] {
		f.match = (f.match + 1) % len(f.matches)
		f.Value = f.matches[f.match]
		return
	}
	if f.Value == "" && f.Default != "" {
		f.Value = f.Default
		return
	}
	m := f.completions()
	if len(m) == 0 {
		return
	}
	f.Value = m[0]
	if len(m) == 1 {
		f.matches = nil
		return
	}
	f.matches, f.match = m, 0
}

// form is a sequence of fields filled one at a time on the status line.
// Validate, when set, looks at the values on submit; a message keeps the
// form open at the field it names.
type form struct {
	Title    string
	Fields   []field
	Cur      int
	Err      string
	Validate func(values []string) (field int, msg string)
	Done     func(values []string) tea.Cmd
}

// handleKey edits the current field. It reports whether the form finished,
// and the command the finish produced.
func (f *form) handleKey(msg tea.KeyPressMsg) (finished bool, cmd tea.Cmd) {
	cur := &f.Fields[f.Cur]
	f.Err = ""
	if msg.String() != "tab" {
		cur.matches = nil
	}
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
		if f.Validate != nil {
			if i, msg := f.Validate(values); msg != "" {
				f.Cur, f.Err = i, msg
				return false, nil
			}
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
		cur.complete()
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
	f.Fields[f.Cur].matches = nil
}
