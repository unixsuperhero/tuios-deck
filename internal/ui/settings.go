package ui

import (
	"errors"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/unixsuperhero/tuios-deck/internal/config"
)

// SetSettings attaches the loaded settings and the file they save to.
func (m *Model) SetSettings(c *config.Config, path string) {
	m.settings, m.settingsPath = c, path
	m.settingsKeys = map[string]map[string]config.Target{}
	for _, mode := range config.Modes() {
		m.settingsKeys[mode] = c.Keymap(mode)
	}
}

func (m *Model) vimModes() bool { return m.settings != nil && m.settings.VimModes }

// keyFromName builds the key press a default key name stands for, so the
// handlers keep matching on their own default keys.
func keyFromName(name string) tea.KeyPressMsg {
	switch name {
	case "tab":
		return tea.KeyPressMsg(tea.Key{Code: tea.KeyTab})
	case "shift+tab":
		return tea.KeyPressMsg(tea.Key{Code: tea.KeyTab, Mod: tea.ModShift})
	case "enter":
		return tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter})
	case "esc":
		return tea.KeyPressMsg(tea.Key{Code: tea.KeyEscape})
	}
	if rest, ok := strings.CutPrefix(name, "ctrl+"); ok && len(rest) == 1 {
		return tea.KeyPressMsg(tea.Key{Code: rune(rest[0]), Mod: tea.ModCtrl})
	}
	r := []rune(name)[0]
	return tea.KeyPressMsg(tea.Key{Code: r, Text: name})
}

// remap turns a key press into the one its default binding would have been.
// It reports false when the key means nothing now. onDeck is whether the
// deck page has the keys; the other pages answer only global actions.
func (m *Model) remap(mode string, msg tea.KeyPressMsg, onDeck bool) (tea.KeyPressMsg, bool) {
	t, ok := m.settingsKeys[mode][msg.String()]
	if !ok || (!t.Global && !onDeck) {
		return msg, true
	}
	if t.Key == "" {
		return msg, false
	}
	return keyFromName(t.Key), true
}

// vimFormKey handles a key press in a form while vim modes are on. In
// insert mode every key types as before and esc goes to normal mode (after
// closing an open `@` list); in normal mode the keys are commands and
// nothing types. A key it returns unhandled goes on to the form's own
// handling, translated to the default key that does it.
func (m *Model) vimFormKey(msg tea.KeyPressMsg) (tea.KeyPressMsg, bool) {
	f := m.form
	if !f.vimNormal {
		msg, ok := m.remap(config.ModeFormInsert, msg, true)
		if !ok {
			return msg, true
		}
		if msg.String() == "esc" {
			if !f.escape() {
				f.vimNormal = true
			}
			return msg, true
		}
		return msg, false
	}
	msg, ok := m.remap(config.ModeFormNormal, msg, true)
	if !ok {
		return msg, true
	}
	cur := &f.Fields[f.Cur]
	switch msg.String() {
	case "i", "a":
		f.vimNormal = false
	case "j", "down":
		if f.Cur+1 < len(f.Fields) {
			f.Cur++
		}
	case "k", "up":
		if f.Cur > 0 {
			f.Cur--
		}
	case "D":
		cur.Value = ""
		f.edited("")
	case "enter":
		return keyFromName("ctrl+s"), false
	case "esc", "ctrl+c", "ctrl+s":
		return msg, false
	}
	return msg, true
}

// vimFormHelp is the form's key line with the mode in it.
func (m *Model) vimFormHelp(plain string) string {
	if !m.vimModes() || m.form == nil {
		return plain
	}
	key := func(mode, action string) string { return m.settings.Binding(mode, action) }
	if m.form.vimNormal {
		pos, _, _ := strings.Cut(plain, " · ")
		return pos + " · NORMAL · " + key(config.ModeFormNormal, "insert") + " insert · " +
			key(config.ModeFormNormal, "next_field") + "/" + key(config.ModeFormNormal, "prev_field") + " field · " +
			key(config.ModeFormNormal, "submit") + " submit · " + key(config.ModeFormNormal, "clear") + " clear · " +
			key(config.ModeFormNormal, "cancel") + " cancel"
	}
	esc := key(config.ModeFormInsert, "leave_insert")
	pos, rest, _ := strings.Cut(plain, " · ")
	return pos + " · INSERT · " + strings.Replace(strings.Replace(rest, "esc cancel", esc+" normal mode", 1), "esc close the list", esc+" close the list", 1)
}

// openSettingsForm edits the settings: vim modes on or off, and one
// rebinding at a time (an action with an empty key goes back to its
// default). The change is checked, saved to the settings file, and takes
// effect at once.
func (m *Model) openSettingsForm() tea.Cmd {
	if m.settings == nil {
		m.setStatus("settings are not loaded")
		return nil
	}
	state := map[bool]string{true: "on", false: "off"}
	curMode := func() string {
		if m.form == nil {
			return ""
		}
		return strings.TrimSpace(m.form.Fields[1].Value)
	}
	m.mode = modeForm
	m.form = &form{
		Title: "settings",
		Fields: []field{
			{Label: "vim modes", Default: state[m.settings.VimModes], Options: func() []string { return []string{"on", "off"} },
				Hint: "on: forms have normal and insert modes; esc leaves insert, esc again cancels"},
			{Label: "mode", Keep: true, Options: func() []string { return config.Modes() },
				Hint: "rebind a key in deck, form_normal or form_insert; leave empty to change only vim modes"},
			{Label: "action", Keep: true, Options: func() []string {
				var out []string
				for _, a := range config.ActionsOf(curMode()) {
					out = append(out, a.Name)
				}
				return out
			}, HintFn: func() string {
				name := strings.TrimSpace(m.form.Fields[2].Value)
				for _, a := range config.ActionsOf(curMode()) {
					if a.Name == name {
						return a.Desc + " · now " + m.settings.Binding(curMode(), name) + ", default " + a.Default
					}
				}
				return ""
			}},
			{Label: "key", Keep: true, Hint: "one character, tab, esc, ctrl+a …; empty restores the default"},
		},
		Validate: func(v []string) (int, string) {
			if _, err := m.candidateSettings(v); err != nil {
				return 3, err.Error()
			}
			return 0, ""
		},
		Done: func(v []string) tea.Cmd {
			c, _ := m.candidateSettings(v)
			if err := c.Save(m.settingsPath); err != nil {
				m.setStatus("settings not saved: " + err.Error())
				return nil
			}
			m.SetSettings(c, m.settingsPath)
			m.setStatus("settings saved to " + m.settingsPath)
			return nil
		},
	}
	return nil
}

// candidateSettings is a copy of the settings with the form's change made.
func (m *Model) candidateSettings(v []string) (*config.Config, error) {
	c := &config.Config{VimModes: m.settings.VimModes, Keys: map[string]map[string]string{}}
	for mode, keys := range m.settings.Keys {
		c.Keys[mode] = map[string]string{}
		for a, k := range keys {
			c.Keys[mode][a] = k
		}
	}
	switch v[0] {
	case "on":
		c.VimModes = true
	case "off":
		c.VimModes = false
	default:
		return nil, errors.New("vim modes is on or off")
	}
	if v[1] != "" || v[2] != "" || v[3] != "" {
		if err := c.Bind(v[1], v[2], v[3]); err != nil {
			return nil, err
		}
	}
	return c, c.Validate()
}
