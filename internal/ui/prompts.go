package ui

import (
	"errors"
	"os"
	"os/exec"
	"runtime"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/unixsuperhero/tuios-deck/internal/panes"
	"github.com/unixsuperhero/tuios-deck/internal/store"
)

// promptModal is the list over the deck that holds the prompt presets or
// the queued prompts: j/k choose, the letters in its help act on the row.
// The row stays on the same id when the list changes under it.
type promptModal struct {
	queue bool
	sel   int
	selID string
	busy  bool // a queued prompt is being sent; its keys wait
}

// dirPickedMsg carries the folder chosen in the operating system's dialog
// to the form that asked for it.
type dirPickedMsg struct {
	label string // the label of the field to fill
	path  string
	err   error
}

// pickDir opens the operating system's folder dialog and reports the
// choice for the field with that label. The dialog is the platform's own,
// so it runs beside the deck without a custom file browser.
func pickDir(label, start string) tea.Cmd {
	return func() tea.Msg {
		var cmd *exec.Cmd
		switch {
		case runtime.GOOS == "darwin":
			script := `POSIX path of (choose folder with prompt "Choose a folder")`
			if st, err := os.Stat(start); err == nil && st.IsDir() {
				script = `POSIX path of (choose folder with prompt "Choose a folder" default location POSIX file "` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(start) + `")`
			}
			cmd = exec.Command("osascript", "-e", script)
		case have("zenity"):
			cmd = exec.Command("zenity", "--file-selection", "--directory", "--filename="+strings.TrimSuffix(start, "/")+"/")
		case have("kdialog"):
			cmd = exec.Command("kdialog", "--getexistingdirectory", start)
		default:
			return dirPickedMsg{label: label, err: errors.New("no folder dialog on this system; type the path")}
		}
		out, err := cmd.Output()
		if err != nil {
			return dirPickedMsg{label: label, err: errors.New("no folder chosen")}
		}
		p := strings.TrimSpace(string(out))
		if len(p) > 1 {
			p = strings.TrimSuffix(p, "/")
		}
		return dirPickedMsg{label: label, path: p}
	}
}

func have(prog string) bool {
	_, err := exec.LookPath(prog)
	return err == nil
}

// fillPickedDir puts the chosen folder in the open form's field.
func (m *Model) fillPickedDir(msg dirPickedMsg) {
	if msg.err != nil {
		m.setStatus(msg.err.Error())
		return
	}
	if m.mode != modeForm || m.form == nil {
		return
	}
	for i := range m.form.Fields {
		if m.form.Fields[i].Label == msg.label {
			m.form.Fields[i].Value = msg.path
			m.form.Cur = i
		}
	}
}

// cwdKey makes ctrl+o in the form open the folder dialog for the field
// labelled label.
func (m *Model) cwdKey(f *form, label string) {
	f.OnKey = func(key string) bool {
		if key != "ctrl+o" {
			return false
		}
		start := ""
		for _, fl := range f.Fields {
			if fl.Label == label {
				start = strings.TrimSpace(fl.Value)
			}
		}
		if start == "" {
			start = cwdDefault(m.selected())
		}
		f.pending = pickDir(label, start)
		return true
	}
}

// presetNames is every preset's name, for completion.
func (m *Model) presetNames() []string {
	var out []string
	for _, p := range m.store.Presets() {
		out = append(out, p.Name)
	}
	return out
}

// presetField is the field that chooses a preset by name; empty is none.
func (m *Model) presetField(hint string) field {
	return field{Label: "preset", Keep: true, Options: m.presetNames, Hint: hint}
}

// resolvePreset finds the preset a field names. Empty means none, which is
// not an error; a name that is not a preset is.
func (m *Model) resolvePreset(name string) (*store.Preset, string) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, ""
	}
	if p := m.store.PresetByName(name); p != nil {
		return p, ""
	}
	return nil, "no preset " + name + " (tab lists them)"
}

// presetPrefix puts the preset's prefix, once, in front of a prompt. An
// empty prompt stays empty: the prefix alone is not a prompt.
func presetPrefix(pr *store.Preset, text string) string {
	if pr == nil || strings.TrimSpace(text) == "" {
		return text
	}
	return store.ApplyPrefix(pr.Prefix, text)
}

// cwdHint says where an empty cwd field will start, given the preset, the
// task and the project the form's fields name right now.
func (m *Model) cwdHint(f *form, presetIdx, taskIdx, projectIdx int, p *panes.Pane) func() string {
	return func() string {
		pr, _ := m.resolvePreset(f.Fields[presetIdx].Value)
		return "empty starts in " + m.startDir("", pr, f.Fields[taskIdx].result(), f.Fields[projectIdx].result(), p) + " · ctrl+o chooses a folder"
	}
}

// openPresetModal lists the presets.
func (m *Model) openPresetModal() tea.Cmd {
	m.mode = modePrompts
	m.pm = &promptModal{}
	return nil
}

func (m *Model) presetRows() []string {
	var out []string
	for _, p := range m.store.Presets() {
		cwd := p.Cwd
		if cwd == "" {
			cwd = "(cwd unchanged)"
		}
		out = append(out, p.Name+"  — "+panes.OneLine(p.Prefix)+"  · "+cwd)
	}
	return out
}

// modalIDs is the ids of the rows the modal shows.
func (m *Model) modalIDs() []string {
	var out []string
	if m.pm.queue {
		for _, q := range m.store.Queued() {
			out = append(out, q.ID)
		}
		return out
	}
	for _, p := range m.store.Presets() {
		out = append(out, p.ID)
	}
	return out
}

// settleModal keeps the selection on its id, else at its place.
func (m *Model) settleModal() []string {
	ids := m.modalIDs()
	for i, id := range ids {
		if id == m.pm.selID {
			m.pm.sel = i
		}
	}
	if m.pm.sel >= len(ids) {
		m.pm.sel = len(ids) - 1
	}
	if m.pm.sel < 0 {
		m.pm.sel = 0
	}
	if m.pm.sel < len(ids) {
		m.pm.selID = ids[m.pm.sel]
	}
	return ids
}

// modalBox draws the modal as a box over the body.
func (m *Model) modalBox(width, maxH int) []string {
	iw := width - 4
	title, rows := "prompt presets", m.presetRows()
	empty := "no presets: n makes one, x adds the short-answer example"
	if m.pm.queue {
		title, rows = "queued prompts", m.queueRows()
		empty = "nothing queued: n queues a prompt for a task"
	}
	m.settleModal()
	lines := []string{boxTop(title, width)}
	if len(rows) == 0 {
		lines = append(lines, boxRow(cDim.Render(empty), iw))
	}
	avail := maxH - 4
	if avail < 1 {
		avail = 1
	}
	start := 0
	if m.pm.sel >= avail {
		start = m.pm.sel - avail + 1
	}
	for i := start; i < len(rows) && i < start+avail; i++ {
		l := ansi.Truncate(rows[i], iw-2, "…")
		if i == m.pm.sel {
			l = cSel.Render(pad("▸ "+ansi.Strip(l), iw))
		} else {
			l = "  " + l
		}
		lines = append(lines, boxRow(l, iw))
	}
	return append(lines,
		cBorder.Render("├"+strings.Repeat("─", width-2)+"┤"),
		boxRow(cDim.Render(ansi.Truncate(keyHelpHint, iw, "…")), iw),
		cBorder.Render("╰"+strings.Repeat("─", width-2)+"╯"),
	)
}

// handleModalKey answers the keys of the open modal.
func (m *Model) handleModalKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	ids := m.settleModal()
	key := msg.String()
	switch key {
	case "esc", "q", "ctrl+c":
		m.mode, m.pm = modeNormal, nil
		return m, nil
	case "j", "down", "ctrl+n":
		if m.pm.sel+1 < len(ids) {
			m.pm.sel++
			m.pm.selID = ids[m.pm.sel]
		}
		return m, nil
	case "k", "up", "ctrl+p":
		if m.pm.sel > 0 {
			m.pm.sel--
			m.pm.selID = ids[m.pm.sel]
		}
		return m, nil
	}
	if m.pm.queue {
		return m, m.handleQueueKey(key, ids)
	}
	switch key {
	case "n":
		return m, m.openPresetForm(nil)
	case "x":
		return m, m.addExamplePreset()
	case "e", "enter":
		if len(ids) > 0 {
			return m, m.openPresetForm(m.store.Preset(ids[m.pm.sel]))
		}
	case "d":
		if len(ids) > 0 {
			if p := m.store.Preset(ids[m.pm.sel]); p != nil {
				id, name := p.ID, p.Name
				m.mode = modeConfirm
				m.confirm.Text = "delete preset " + name + "? (Y/n)"
				m.confirm.Do = func() tea.Msg {
					if err := m.store.DeletePreset(id); err != nil {
						return modalMsg{reopen: true, err: err}
					}
					return modalMsg{reopen: true, status: "deleted preset " + name}
				}
			}
		}
	}
	return m, nil
}

// modalMsg ends an action started from the modal and brings it back.
type modalMsg struct {
	queue  bool
	reopen bool // show the modal again; a form or a confirmation closed it
	done   bool // a queued prompt was dispatched, whether or not it went
	status string
	err    error
}

// reopenModal shows the modal again after a confirmation or a form, and
// says what happened.
func (m *Model) reopenModal(msg modalMsg) {
	if msg.err != nil {
		m.setStatus("error: " + msg.err.Error())
	} else if msg.status != "" {
		m.setStatus(msg.status)
	}
	if msg.done && m.pm != nil {
		m.pm.busy = false
	}
	if !msg.reopen || m.mode != modeNormal && m.mode != modePrompts {
		return
	}
	m.mode = modePrompts
	if m.pm == nil || m.pm.queue != msg.queue {
		m.pm = &promptModal{queue: msg.queue}
	}
}

// addExamplePreset stores the short-answer example, only when asked for.
func (m *Model) addExamplePreset() tea.Cmd {
	ex := store.ExamplePreset()
	if _, err := m.store.AddPreset(ex.Name, ex.Prefix, ex.Cwd); err != nil {
		m.setStatus("error: " + err.Error())
		return nil
	}
	m.setStatus("added preset " + ex.Name)
	return nil
}

// openPresetForm makes a preset, or edits the one given. When it is done
// the modal comes back.
func (m *Model) openPresetForm(edit *store.Preset) tea.Cmd {
	fields := []field{
		{Label: "name"},
		{Label: "cwd", Keep: true, Hint: "where a new agent or terminal starts; empty leaves it to the form · ctrl+o chooses a folder"},
		{Label: "prefix", Keep: true, Multi: true, Hint: "put in front of the prompt, once; a space follows unless it ends in whitespace"},
	}
	title := "new prompt preset"
	if edit != nil {
		title = "edit preset " + edit.Name
		fields[0].Value, fields[1].Value, fields[2].Value = edit.Name, edit.Cwd, edit.Prefix
	}
	m.mode = modeForm
	m.form = &form{
		Title:  title,
		Fields: fields,
		Validate: func(v []string) (int, string) {
			if strings.TrimSpace(v[0]) == "" {
				return 0, "a preset needs a name"
			}
			if p := m.store.PresetByName(v[0]); p != nil && (edit == nil || p.ID != edit.ID) {
				return 0, "a preset named " + p.Name + " exists"
			}
			return 0, ""
		},
		Done: func(v []string) tea.Cmd {
			return func() tea.Msg {
				if edit != nil {
					if err := m.store.UpdatePreset(edit.ID, v[0], v[2], v[1]); err != nil {
						return modalMsg{reopen: true, err: err}
					}
					return modalMsg{reopen: true, status: "saved preset " + v[0]}
				}
				if _, err := m.store.AddPreset(v[0], v[2], v[1]); err != nil {
					return modalMsg{reopen: true, err: err}
				}
				return modalMsg{reopen: true, status: "added preset " + v[0]}
			}
		},
	}
	m.cwdKey(m.form, "cwd")
	return nil
}
