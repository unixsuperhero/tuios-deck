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

// config is the settings in effect: the loaded ones, else the defaults.
func (m *Model) config() *config.Config {
	if m.settings != nil {
		return m.settings
	}
	return &config.Config{}
}

// formReserved is the keys a field's hotkey may not take: the normal-mode
// commands as bound, and the help key.
func (m *Model) formReserved() map[string]bool {
	c := m.config()
	r := map[string]bool{"?": true, c.Binding(config.ModeDeck, "help"): true}
	for _, a := range config.ActionsOf(config.ModeFormNormal) {
		r[c.Binding(config.ModeFormNormal, a.Name)] = true
	}
	return r
}

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

// formModeKey handles a key press in a form. In input mode every key types
// and esc goes back to normal mode (after closing an open list); in normal
// mode the keys are commands and nothing types: a field's hotkey puts the
// form in input mode on that field. A key it returns unhandled goes on to
// the form's own handling, translated to the default key that does it.
func (m *Model) formModeKey(msg tea.KeyPressMsg) (tea.KeyPressMsg, bool) {
	f := m.form
	if f.typing {
		msg, ok := m.remap(config.ModeFormInsert, msg, true)
		if !ok {
			return msg, true
		}
		if msg.String() == "esc" {
			if !f.escape() {
				f.typing = false
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
	key := msg.String()
	switch key {
	case "i":
		f.typing = true
	case "j", "down", "tab":
		f.next(1)
	case "k", "up", "shift+tab":
		f.next(-1)
	case "D":
		defer cur.remember(cur.editState())
		cur.Value = ""
		f.edited("")
	case "enter":
		return keyFromName("ctrl+s"), false
	case "esc", "ctrl+c", "ctrl+s", "ctrl+z", "ctrl+enter":
		return msg, false
	default:
		if f.OnKey != nil && f.OnKey(key) {
			return msg, true
		}
		if i := f.hotkey(key); i >= 0 {
			f.Cur, f.typing = i, true
		}
	}
	return msg, true
}

// openSettingsForm edits the settings: one rebinding at a time (an action
// with an empty key goes back to its default). The change is checked,
// saved to the settings file, and takes effect at once.
func (m *Model) openSettingsForm() tea.Cmd {
	if m.settings == nil {
		m.setStatus("settings are not loaded")
		return nil
	}
	curMode := func() string {
		if m.form == nil {
			return ""
		}
		return strings.TrimSpace(m.form.Fields[0].Value)
	}
	m.mode = modeForm
	m.form = &form{
		Title: "settings",
		Fields: []field{
			{Label: "mode", Keep: true, Options: func() []string { return config.Modes() },
				Hint: "rebind a key in deck, form_normal, form_insert or live"},
			{Label: "action", Keep: true, Options: func() []string {
				var out []string
				for _, a := range config.ActionsOf(curMode()) {
					out = append(out, a.Name)
				}
				return out
			}, HintFn: func() string {
				name := strings.TrimSpace(m.form.Fields[1].Value)
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
				return 2, err.Error()
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
	c := &config.Config{StaleAfter: m.settings.StaleAfter, Keys: map[string]map[string]string{}}
	for mode, keys := range m.settings.Keys {
		c.Keys[mode] = map[string]string{}
		for a, k := range keys {
			c.Keys[mode][a] = k
		}
	}
	if v[0] == "" && v[1] == "" && v[2] == "" {
		return nil, errors.New("name a mode, an action and its key")
	}
	if err := c.Bind(v[0], v[1], v[2]); err != nil {
		return nil, err
	}
	return c, c.Validate()
}
