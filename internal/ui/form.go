package ui

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"
)

// addNew is the last entry of a project or task field's list.
const addNew = "+ add new…"

// listRows is how many entries of a completion list, and how many lines of
// a multiline value, the form box shows at once.
const listRows = 8

// field is one line of a form: a label, the text typed so far, and a default
// used when the person leaves it empty. A field with Options shows the
// options that fuzzy-match the typed text in a list under it. A Multi field
// holds several lines: enter inserts a newline there, and `@` completes a
// file path under the form's cwd. Hint, when set, is
// shown under the field while it is the current one. New, "project" or
// "task", ends the list with addNew, which opens the mass-add screen.
type field struct {
	Label   string
	Value   string
	Default string
	Keep    bool // keep an empty value empty instead of taking the default
	Hint    string
	Options func() []string
	Multi   bool
	New     string

	hl int // the highlighted list entry plus one; 0 is none
	at int // the offset of the '@' being completed plus one; 0 is none
}

func (f field) result() string {
	if f.Value == "" && !f.Keep {
		return f.Default
	}
	return f.Value
}

// isOption says whether the value is one of the options, ignoring case.
func (f field) isOption() bool {
	if f.Options == nil {
		return false
	}
	v := strings.TrimSpace(f.Value)
	for _, o := range f.Options() {
		if strings.EqualFold(o, v) {
			return true
		}
	}
	return false
}

// fuzzy is the options the query matches, best rank first and in their
// listed order within a rank, the way the palette ranks. An empty query
// keeps them all.
func fuzzy(options []string, query string) []string {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return options
	}
	type scored struct {
		rank int
		opt  string
	}
	var out []scored
	for _, o := range options {
		if r := matchRank(strings.ToLower(o), q); r >= 0 && r < 4 {
			out = append(out, scored{r, o})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].rank < out[j].rank })
	res := make([]string, len(out))
	for i, s := range out {
		res[i] = s.opt
	}
	return res
}

// form is a sequence of fields, drawn as a box over the body. Validate,
// when set, looks at the values on submit; a message keeps the form open
// at the field it names.
type form struct {
	Title    string
	Fields   []field
	Cur      int
	Err      string
	Validate func(values []string) (field int, msg string)
	Done     func(values []string) tea.Cmd

	adding int // the field whose addNew was taken, plus one; 0 is none

	cwd  string              // the folder `@` completes under
	dirs map[string][]string // the entries read so far, by folder under cwd
}

// maxEntries caps how many entries of one folder `@` lists.
const maxEntries = 200

// splitPath cuts what follows an `@` into the folder typed so far, ending
// in a slash or empty, and the name being typed in it.
func splitPath(rel string) (dir, name string) {
	i := strings.LastIndex(rel, "/")
	return rel[:i+1], rel[i+1:]
}

// entries is the folder dir under the cwd, read once per form: folders
// with a trailing slash, files plain, hidden ones left out. A symlink is
// listed only when it points inside the cwd, and a folder outside it is
// never read.
func (f *form) entries(dir string) []string {
	if e, ok := f.dirs[dir]; ok {
		return e
	}
	if f.dirs == nil {
		f.dirs = map[string][]string{}
	}
	f.dirs[dir] = readEntries(f.cwd, dir)
	return f.dirs[dir]
}

func readEntries(cwd, dir string) []string {
	root, err := filepath.EvalSymlinks(cwd)
	if err != nil {
		return nil
	}
	inside := func(p string) bool {
		return p == root || strings.HasPrefix(p, root+string(filepath.Separator))
	}
	full, err := filepath.EvalSymlinks(filepath.Join(root, dir))
	if err != nil || !inside(full) {
		return nil
	}
	ents, err := os.ReadDir(full)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range ents {
		name := e.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		isDir := e.IsDir()
		if e.Type()&os.ModeSymlink != 0 {
			target, err := filepath.EvalSymlinks(filepath.Join(full, name))
			if err != nil || !inside(target) {
				continue
			}
			st, err := os.Stat(target)
			if err != nil {
				continue
			}
			isDir = st.IsDir()
		}
		if isDir {
			name += "/"
		}
		out = append(out, name)
		if len(out) == maxEntries {
			break
		}
	}
	return out
}

// list is the completion list open under the current field, nil when none.
func (f *form) list() []string {
	cur := f.Fields[f.Cur]
	if cur.at > 0 {
		dir, name := splitPath(cur.Value[cur.at:])
		return fuzzy(f.entries(dir), name)
	}
	if cur.Options == nil {
		return nil
	}
	if cur.New != "" {
		return append(fuzzy(cur.Options(), cur.Value), addNew)
	}
	return fuzzy(cur.Options(), cur.Value)
}

// accept puts the highlighted entry, or the first, in the field. With
// nothing highlighted an empty field takes its default instead.
func (f *form) accept(list []string) {
	cur := &f.Fields[f.Cur]
	i := cur.hl - 1
	if i < 0 {
		if cur.Value == "" && cur.Default != "" {
			cur.Value = cur.Default
			return
		}
		i = 0
	}
	if cur.at > 0 {
		// A folder is descended into; a file replaces the @query with its
		// path relative to the cwd and closes the list.
		dir, _ := splitPath(cur.Value[cur.at:])
		if strings.HasSuffix(list[i], "/") {
			cur.Value, cur.hl = cur.Value[:cur.at]+dir+list[i], 1
			return
		}
		cur.Value, cur.at, cur.hl = cur.Value[:cur.at-1]+dir+list[i], 0, 0
		return
	}
	if list[i] == addNew {
		f.adding, cur.hl = f.Cur+1, 0
		return
	}
	cur.Value, cur.hl = list[i], 0
}

// escape closes an open `@` list and reports whether there was one, so
// esc closes the list before it cancels the form.
func (f *form) escape() bool {
	cur := &f.Fields[f.Cur]
	if cur.at == 0 {
		return false
	}
	cur.at, cur.hl = 0, 0
	return true
}

// submit validates the values and, when they pass, finishes the form.
func (f *form) submit() (bool, tea.Cmd) {
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
}

// edited resets the highlight after the value changed: the best match is
// highlighted while there is text to match. An `@` typed at the start of a
// word of a Multi field opens the path list; a space or a newline after it,
// or deleting it, closes the list.
func (f *form) edited(typed string) {
	cur := &f.Fields[f.Cur]
	if cur.at > 0 && (len(cur.Value) < cur.at || strings.ContainsAny(cur.Value[cur.at:], " \t\n")) {
		cur.at = 0
	}
	if cur.Multi && typed == "@" {
		before := cur.Value[:len(cur.Value)-1]
		if before == "" || strings.ContainsAny(before[len(before)-1:], " \t\n") {
			cur.at = len(cur.Value)
		}
	}
	cur.hl = 0
	// addNew is never highlighted on its own, so enter on a name that
	// matches nothing is refused rather than opening the mass-add screen.
	if list := f.list(); cur.Value != "" && len(list) > 0 && list[0] != addNew {
		cur.hl = 1
	}
}

// handleKey edits the current field. It reports whether the form finished,
// and the command the finish produced.
func (f *form) handleKey(msg tea.KeyPressMsg) (finished bool, cmd tea.Cmd) {
	cur := &f.Fields[f.Cur]
	f.Err = ""
	key := msg.String()
	if list := f.list(); len(list) > 0 {
		switch key {
		case "ctrl+n", "down":
			cur.hl = cur.hl%len(list) + 1
			return false, nil
		case "ctrl+p", "up":
			if cur.hl <= 1 {
				cur.hl = len(list)
			} else {
				cur.hl--
			}
			return false, nil
		case "tab":
			f.accept(list)
			return false, nil
		case "enter":
			if cur.hl > 0 && !cur.isOption() {
				f.accept(list)
				return false, nil
			}
		}
	}
	switch key {
	case "ctrl+s", "ctrl+enter":
		return f.submit()
	case "enter":
		if cur.Multi {
			cur.Value += "\n"
			return false, nil
		}
		if f.Cur+1 < len(f.Fields) {
			f.Cur++
			return false, nil
		}
		return f.submit()
	case "shift+tab", "up":
		if f.Cur > 0 {
			f.Cur--
		}
		return false, nil
	case "tab":
		if cur.Value == "" && cur.Default != "" {
			cur.Value = cur.Default
		}
		return false, nil
	case "backspace":
		if r := []rune(cur.Value); len(r) > 0 {
			cur.Value = string(r[:len(r)-1])
		}
	case "ctrl+u":
		cur.Value = ""
	case "ctrl+w":
		cur.Value = strings.TrimRight(cur.Value, " ")
		if i := strings.LastIndexAny(cur.Value, " \n"); i >= 0 {
			cur.Value = cur.Value[:i+1]
		} else {
			cur.Value = ""
		}
	default:
		k := msg.Key()
		if k.Text == "" || k.Mod != 0 {
			return false, nil
		}
		cur.Value += k.Text
		f.edited(k.Text)
		return false, nil
	}
	f.edited("")
	return false, nil
}

// paste adds text to the current field. A Multi field keeps its newlines;
// any other field gets them as spaces.
func (f *form) paste(text string) {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	if !f.Fields[f.Cur].Multi {
		text = strings.ReplaceAll(text, "\n", " ")
	}
	f.Fields[f.Cur].Value += text
	f.edited("")
}
