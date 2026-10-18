package ui

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
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
// Outline, on a Multi field, gives it a line cursor: the arrows move it,
// typing and enter edit at it, tab and shift+tab indent and outdent its
// line by one level, and `@` completes nothing.
type field struct {
	Label   string
	Value   string
	Default string
	Keep    bool // keep an empty value empty instead of taking the default
	Hint    string
	Options func() []string
	Multi   bool
	New     string
	HintFn  func() string // the hint worked out afresh on every draw, in place of Hint
	Outline bool
	// Preview, when set, works out a preview of the highlighted option: metadata
	// and a pane's recent output, drawn under the list. Options is unchanged.
	Preview func(option string) previewSpec
	// Attach, on a Multi field, makes `@` complete a file the harness reads as
	// a reference: the `@` stays in front of the path, and ctrl+o picks files
	// with the OS file dialog. Slash, when set, lists the slash commands and
	// skills that a `/` typed as the first character completes.
	Attach bool
	Slash  func() []string
	// Known, on an Outline field, gives for each line the status of the
	// existing item it names, "" for none; such a line is drawn dim, with
	// the status when it is final.
	Known func(value string) []string

	hl   int // the highlighted list entry plus one; 0 is none
	at   int // the offset of the '@' being completed plus one; 0 is none
	sl   int // the offset after the leading '/' being completed; 0 is none
	pos  int // an Outline field's cursor, a byte offset into Value
	top  int // the first of an Outline field's lines in sight
	undo []fieldEdit
}

type fieldEdit struct {
	value                string
	hl, at, sl, pos, top int
}

func (f *field) editState() fieldEdit {
	return fieldEdit{f.Value, f.hl, f.at, f.sl, f.pos, f.top}
}

func (f *field) remember(before fieldEdit) {
	if f.Value != before.value {
		f.undo = append(f.undo, before)
	}
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
	// OnKey, when set, sees every key first from any field; a key it
	// reports handled goes no further.
	OnKey func(key string) bool
	// pending is a command OnKey asked for; handleKey returns it.
	pending tea.Cmd

	vimNormal bool // normal mode of the vim modes setting; false is insert

	adding     int    // the field whose addNew was taken, plus one; 0 is none
	targetTask string // last task value seen by project autofill; project edits stay editable

	cwd   string              // the folder `@` completes under
	cwdFn func() string       // when set, the folder worked out afresh instead of cwd
	dirs  map[string][]string // the entries read so far, by folder under cwd
}

// root is the folder `@` completes under.
func (f *form) root() string {
	if f.cwdFn != nil {
		if c := f.cwdFn(); c != "" {
			return c
		}
	}
	return f.cwd
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
	key := f.root() + "\x00" + dir
	if e, ok := f.dirs[key]; ok {
		return e
	}
	if f.dirs == nil {
		f.dirs = map[string][]string{}
	}
	f.dirs[key] = readEntries(f.root(), dir)
	return f.dirs[key]
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

// atQuery is what follows the `@` being completed, and whether it opened
// with a double quote, in which case it is the text after the quote. In an
// Attach field the quoted form may hold spaces.
func (f field) atQuery() (q string, quoted bool) {
	q = f.Value[f.at:]
	if f.Attach && strings.HasPrefix(q, `"`) {
		return q[1:], true
	}
	return q, false
}

// atDone says the `@` list should close: the `@` was deleted, a quote was
// closed, or whitespace ended an unquoted path.
func (f field) atDone() bool {
	if len(f.Value) < f.at {
		return true
	}
	q, quoted := f.atQuery()
	if quoted {
		return strings.ContainsAny(q, `"`+"\n")
	}
	return strings.ContainsAny(q, " \t\n")
}

// list is the completion list open under the current field, nil when none.
func (f *form) list() []string {
	cur := f.Fields[f.Cur]
	if cur.sl > 0 {
		return fuzzy(cur.Slash(), cur.Value[cur.sl:])
	}
	if cur.at > 0 {
		q, _ := cur.atQuery()
		dir, name := splitPath(q)
		ents := f.entries(dir)
		if cur.Attach {
			// No @ form can hold a double quote or a newline in a name.
			var ok []string
			for _, e := range ents {
				if !strings.ContainsAny(e, "\"\n") {
					ok = append(ok, e)
				}
			}
			ents = ok
		}
		return fuzzy(ents, name)
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
	if cur.sl > 0 {
		cur.Value, cur.sl, cur.hl = cur.Value[:cur.sl]+list[i]+" ", 0, 0
		return
	}
	if cur.at > 0 {
		// A folder is descended into; a file replaces the @query with its
		// path relative to the cwd and closes the list. An Attach field keeps
		// the @ and ends the reference with a space.
		q, quoted := cur.atQuery()
		dir, _ := splitPath(q)
		if cur.Attach {
			path := dir + list[i]
			isDir := strings.HasSuffix(list[i], "/")
			if quoted || strings.ContainsAny(path, " \t") {
				// A path with whitespace is written @"path"; a folder leaves the
				// quote open so the list goes on inside it.
				if isDir {
					cur.Value, cur.hl = cur.Value[:cur.at]+`"`+path, 1
				} else {
					cur.Value, cur.at, cur.hl = cur.Value[:cur.at]+`"`+path+`" `, 0, 0
				}
				return
			}
			if isDir {
				cur.Value, cur.hl = cur.Value[:cur.at]+path, 1
				return
			}
			cur.Value, cur.at, cur.hl = cur.Value[:cur.at]+path+" ", 0, 0
			return
		}
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
	if cur.at == 0 && cur.sl == 0 {
		return false
	}
	cur.at, cur.sl, cur.hl = 0, 0, 0
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
	if cur.at > 0 && cur.atDone() {
		cur.at = 0
	}
	if cur.sl > 0 && (len(cur.Value) < cur.sl || strings.ContainsAny(cur.Value[cur.sl:], " \t\n")) {
		cur.sl = 0
	}
	if cur.Slash != nil && typed == "/" && cur.Value == "/" {
		cur.sl = 1
	}
	if cur.Multi && !cur.Outline && typed == "@" {
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
	if key == "ctrl+z" {
		if n := len(cur.undo); n > 0 {
			previous := cur.undo[n-1]
			cur.undo[n-1] = fieldEdit{}
			cur.undo = cur.undo[:n-1]
			cur.Value, cur.hl, cur.at, cur.sl, cur.pos, cur.top = previous.value, previous.hl, previous.at, previous.sl, previous.pos, previous.top
		}
		return false, nil
	}
	defer cur.remember(cur.editState())
	if f.OnKey != nil && f.OnKey(key) {
		cmd, f.pending = f.pending, nil
		return false, cmd
	}
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
	if cur.Multi && cur.Outline && cur.outlineKey(key) {
		f.edited("")
		return false, nil
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
		cur.Value = dropLastGrapheme(cur.Value)
	case "ctrl+u":
		cur.Value = cur.Value[:strings.LastIndex(cur.Value, "\n")+1]
	case "ctrl+w":
		cur.Value = strings.TrimRight(cur.Value, " ")
		if i := strings.LastIndexAny(cur.Value, " \n"); i >= 0 {
			cur.Value = cur.Value[:i+1]
		} else {
			cur.Value = ""
		}
	default:
		k := msg.Key()
		// Shift (and the lock keys) only change which text arrives; ctrl, alt
		// and the like are shortcuts, not typing.
		if k.Text == "" || k.Mod&^(tea.ModShift|tea.ModCapsLock|tea.ModNumLock|tea.ModScrollLock) != 0 {
			return false, nil
		}
		if cur.Outline {
			cur.insert(k.Text)
		} else {
			cur.Value += k.Text
		}
		f.edited(k.Text)
		return false, nil
	}
	f.edited("")
	return false, nil
}

// dropLastGrapheme is s without its last grapheme, so backspace takes a
// whole glyph, an emoji with its variation selector or a letter with its
// combining accent, rather than leaving half of one behind.
func dropLastGrapheme(s string) string {
	last := 0
	for rest := s; rest != ""; {
		g, _ := ansi.FirstGraphemeCluster(rest, ansi.GraphemeWidth)
		if g == "" {
			break
		}
		last = len(s) - len(rest)
		rest = rest[len(g):]
	}
	return s[:last]
}

// indentLast puts two spaces at the start of the last line of v, or with
// in false takes one level off it: a tab, two spaces, or a lone space.
// A line with no indent stays as it is.
func indentLast(v string, in bool) string {
	i := strings.LastIndex(v, "\n") + 1
	head, line := v[:i], v[i:]
	switch {
	case in:
		line = "  " + line
	case strings.HasPrefix(line, "\t"):
		line = line[1:]
	case strings.HasPrefix(line, "  "):
		line = line[2:]
	case strings.HasPrefix(line, " "):
		line = line[1:]
	}
	return head + line
}

// paste adds text to the current field. A Multi field keeps its newlines;
// any other field gets them as spaces.
func (f *form) paste(text string) {
	cur := &f.Fields[f.Cur]
	defer cur.remember(cur.editState())
	text = strings.ReplaceAll(text, "\r\n", "\n")
	if !f.Fields[f.Cur].Multi {
		text = strings.ReplaceAll(text, "\n", " ")
	}
	if cur := &f.Fields[f.Cur]; cur.Outline {
		cur.insert(text)
	} else {
		cur.Value += text
	}
	f.edited("")
}
