package config

import (
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"
)

// The modes keys are bound in. deck is the dashboard and its pages; the
// form modes apply only while vim_modes is on.
const (
	ModeDeck       = "deck"
	ModeFormNormal = "form_normal"
	ModeFormInsert = "form_insert"
)

// Action is something a key does: its name in the file, the key it has by
// default, and what it does. Global actions answer on every page; the rest
// only on the deck page.
type Action struct {
	Name, Default, Desc string
	Global              bool
}

var actions = map[string][]Action{
	ModeDeck: {
		{"help", "?", "show keybindings and actions", true},
		{"quit", "q", "leave the deck", true},
		{"palette", ":", "open the command palette", true},
		{"tasks", "T", "go to the tasks page", true},
		{"focus", "F", "focus a task or project, or unlock", true},
		{"focus_plan", "ctrl+f", "toggle the saved easy-task focus plan", true},
		{"captures", "C", "go to the captures page", true},
		{"add_many", "A", "add projects and tasks from an outline", true},
		{"context", "I", "go to the context page", true},
		{"review", "V", "go to the finished prompt review queue", true},
		{"history", "H", "go to the history timeline", true},
		{"reset_view", "R", "reset page filters and sorting without deleting data or unlocking focus", true},
		{"left", "h", "focus the list", false},
		{"right", "l", "focus the detail", false},
		{"down", "j", "next pane, or scroll the detail", false},
		{"up", "k", "previous pane, or scroll the detail", false},
		{"top", "g", "first pane, or the detail's top", false},
		{"bottom", "G", "follow the detail's end", false},
		{"scroll_down", "J", "scroll the detail down half a page", false},
		{"scroll_up", "K", "scroll the detail up half a page", false},
		{"next_tab", "tab", "next tab", false},
		{"prev_tab", "shift+tab", "previous tab", false},
		{"refresh", "r", "list the sessions and agents again", false},
		{"group_next", "o", "next grouping of the list", false},
		{"group_prev", "O", "previous grouping of the list", false},
		{"sort_next", "s", "next sort of the rail panes", false},
		{"new_agent", "n", "start an agent", false},
		{"new_terminal", "t", "open a shell", false},
		{"prompt", "p", "send a prompt", false},
		{"assign", "a", "assign the pane to a task or project", false},
		{"capture", "c", "capture the pane's screen", false},
		{"extract", "i", "extract context from the pane", false},
		{"close", "x", "close the pane", false},
	},
	ModeFormNormal: {
		{"insert", "i", "type in the current field", false},
		{"append", "a", "type in the current field", false},
		{"next_field", "j", "the next field", false},
		{"prev_field", "k", "the previous field", false},
		{"clear", "D", "empty the current field", false},
		{"submit", "enter", "submit the form", false},
		{"cancel", "esc", "cancel the form", false},
	},
	ModeFormInsert: {
		{"leave_insert", "esc", "back to normal mode", false},
	},
}

// reserved are keys a mode answers itself and a binding may not take.
var reserved = map[string][]string{
	ModeDeck:       {"1", "2", "3", "4", "[", "]", "enter", "esc", "up", "down", "left", "right", "home", "end", "pgup", "pgdown", "ctrl+d", "ctrl+u", "ctrl+c", "ctrl+p"},
	ModeFormNormal: {"ctrl+c", "ctrl+s", "up", "down", "tab", "shift+tab"},
	ModeFormInsert: {"enter", "backspace", "tab", "shift+tab", "up", "down", "ctrl+c", "ctrl+s", "ctrl+n", "ctrl+p", "ctrl+u", "ctrl+w", "ctrl+enter"},
}

var namedKeys = map[string]bool{"tab": true, "shift+tab": true, "enter": true, "esc": true, "up": true, "down": true,
	"left": true, "right": true, "home": true, "end": true, "pgup": true, "pgdown": true, "backspace": true}

// Modes lists the mode names.
func Modes() []string { return []string{ModeDeck, ModeFormNormal, ModeFormInsert} }

// ActionsOf lists a mode's actions in their documented order.
func ActionsOf(mode string) []Action { return actions[mode] }

func findAction(mode, name string) (Action, bool) {
	for _, a := range actions[mode] {
		if a.Name == name {
			return a, true
		}
	}
	return Action{}, false
}

// validKey says whether s is a key this file may name: one printable
// character, a named key, or ctrl+ or alt+ with a lowercase letter.
func validKey(s string) bool {
	switch {
	case namedKeys[s]:
		return true
	case strings.HasPrefix(s, "ctrl+") || strings.HasPrefix(s, "alt+"):
		rest := s[strings.Index(s, "+")+1:]
		return len(rest) == 1 && rest[0] >= 'a' && rest[0] <= 'z' || s == "ctrl+enter"
	}
	r, n := utf8.DecodeRuneInString(s)
	return n == len(s) && r != utf8.RuneError && r > ' ' && r != 0x7f
}

// printable says whether typing the key puts a character in a field.
func printable(s string) bool {
	r, n := utf8.DecodeRuneInString(s)
	return n == len(s) && r > ' '
}

// Binding is the key an action has in the mode: the configured one, else the default.
func (c *Config) Binding(mode, name string) string {
	if k := c.Keys[mode][name]; k != "" {
		return k
	}
	a, _ := findAction(mode, name)
	return a.Default
}

// Bind sets an action's key; the default key, or an empty one, removes the entry.
func (c *Config) Bind(mode, name, key string) error {
	a, ok := findAction(mode, name)
	if !ok {
		return fmt.Errorf("no action %q in mode %q", name, mode)
	}
	if key == "" || key == a.Default {
		delete(c.Keys[mode], name)
		if len(c.Keys[mode]) == 0 {
			delete(c.Keys, mode)
		}
		return nil
	}
	if c.Keys == nil {
		c.Keys = map[string]map[string]string{}
	}
	if c.Keys[mode] == nil {
		c.Keys[mode] = map[string]string{}
	}
	c.Keys[mode][name] = key
	return nil
}

// validateKeys checks every rebinding: known mode and action, a key that
// can be named, not reserved, never a printable character while typing
// (insert mode), and no two actions of a mode on one key.
func (c *Config) validateKeys() error {
	var modes []string
	for m := range c.Keys {
		modes = append(modes, m)
	}
	sort.Strings(modes)
	for _, m := range modes {
		if _, ok := actions[m]; !ok {
			return fmt.Errorf("keys: unknown mode %q (modes: %s)", m, strings.Join(Modes(), ", "))
		}
		for name, key := range c.Keys[m] {
			if _, ok := findAction(m, name); !ok {
				return fmt.Errorf("keys.%s: unknown action %q", m, name)
			}
			if !validKey(key) {
				return fmt.Errorf("keys.%s.%s: %q is not a key (one character, a name such as tab or esc, or ctrl+a)", m, name, key)
			}
			if m == ModeFormInsert && printable(key) {
				return fmt.Errorf("keys.%s.%s: %q would type into the field; use a ctrl+ or named key", m, name, key)
			}
			for _, r := range reserved[m] {
				if key == r {
					return fmt.Errorf("keys.%s.%s: %q is reserved in %s mode", m, name, key, m)
				}
			}
		}
	}
	for _, m := range Modes() {
		seen := map[string]string{}
		for _, a := range actions[m] {
			k := c.Binding(m, a.Name)
			if other, dup := seen[k]; dup {
				return fmt.Errorf("keys.%s: %q is bound to both %s and %s", m, k, other, a.Name)
			}
			seen[k] = a.Name
		}
	}
	return nil
}

// Target is what a pressed key stands for after rebinding: the default key
// of the action it is bound to, which the existing handlers answer. An
// empty Key means the key does nothing now.
type Target struct {
	Key    string
	Global bool
}

// Keymap is the translation table of a mode: the keys that differ from the
// defaults. A default key whose action moved elsewhere maps to nothing.
func (c *Config) Keymap(mode string) map[string]Target {
	t := map[string]Target{}
	for _, a := range actions[mode] {
		if c.Binding(mode, a.Name) != a.Default {
			t[a.Default] = Target{"", a.Global}
		}
	}
	for _, a := range actions[mode] {
		if k := c.Binding(mode, a.Name); k != a.Default {
			t[k] = Target{a.Default, a.Global}
		}
	}
	return t
}
