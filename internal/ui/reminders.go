package ui

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/unixsuperhero/tuios-deck/internal/store"
)

// reminderLabel is how a reminder is listed in a form's options.
func reminderLabel(r store.Reminder) string {
	state := "off"
	if r.Enabled {
		state = "next " + time.Unix(r.Next, 0).Format("15:04:05")
	}
	return fmt.Sprintf("%s · every %s · %s [%s]", r.Title, store.FormatInterval(r.Interval), state, r.ID)
}

func (m *Model) reminderLabels(keep func(store.Reminder) bool) []string {
	var out []string
	for _, r := range m.store.Reminders() {
		if keep == nil || keep(r) {
			out = append(out, reminderLabel(r))
		}
	}
	return out
}

// reminderOf finds the reminder a label from reminderLabels names, by the
// id in its brackets.
func (m *Model) reminderOf(label string) *store.Reminder {
	label = strings.TrimSpace(label)
	i := strings.LastIndex(label, "[")
	if i < 0 || !strings.HasSuffix(label, "]") {
		return nil
	}
	return m.store.Reminder(label[i+1 : len(label)-1])
}

func (m *Model) reminderCommands() []palItem {
	return []palItem{
		{"deck", "reminders", "list the recurring reminders; they fire while the deck runs", m.showReminders},
		{"deck", "new reminder…", "repeat a notification every so often (terminal notice and OS notification)", m.openReminderForm},
		{"deck", "edit reminder…", "change a reminder's title, message or interval", m.openReminderEdit},
		{"deck", "enable reminder…", "turn a disabled reminder on; its wait restarts", func() tea.Cmd {
			return m.openReminderSwitch(true)
		}},
		{"deck", "disable reminder…", "stop a reminder without deleting it", func() tea.Cmd {
			return m.openReminderSwitch(false)
		}},
		{"deck", "delete reminder…", "delete a reminder", m.openReminderDelete},
	}
}

// showReminders puts the list in the detail pane, where it stays until the
// selection or the tab changes.
func (m *Model) showReminders() tea.Cmd {
	m.page = pageDeck
	m.result = &verbResult{Verb: "reminders"}
	m.detailErr = ""
	m.scroll, m.follow, m.revision = 0, false, 0
	m.detail = []string{"reminders fire only while the deck runs (deck tui or deck web)", ""}
	rs := m.store.Reminders()
	if len(rs) == 0 {
		m.detail = append(m.detail, "none yet: palette → new reminder…")
	}
	for _, r := range rs {
		m.detail = append(m.detail, reminderLabel(r))
		if r.Message != "" {
			m.detail = append(m.detail, "    "+strings.ReplaceAll(r.Message, "\n", " "))
		}
	}
	m.setStatus(fmt.Sprintf("%d reminders", len(rs)))
	return nil
}

func (m *Model) openReminderForm() tea.Cmd {
	m.mode = modeForm
	m.form = &form{
		Title: "new reminder",
		Fields: []field{
			{Label: "title", Keep: true},
			{Label: "message", Keep: true, Hint: "shown in the notification; the title when empty"},
			{Label: "every", Default: "30m", Hint: "30m, 2h, 1d, 90s"},
		},
		Validate: func(v []string) (int, string) {
			if strings.TrimSpace(v[0]) == "" {
				return 0, "title is empty"
			}
			if _, err := store.ParseInterval(v[2]); err != nil {
				return 2, err.Error()
			}
			return 0, ""
		},
		Done: func(v []string) tea.Cmd {
			return func() tea.Msg {
				sec, _ := store.ParseInterval(v[2])
				r, err := m.store.AddReminder(v[0], v[1], sec)
				if err != nil {
					return actionMsg{err: err}
				}
				return actionMsg{status: "reminder " + r.Title + " every " + store.FormatInterval(r.Interval)}
			}
		},
	}
	return nil
}

func (m *Model) pickReminder(labels []string, what string) bool {
	if len(labels) == 0 {
		m.setStatus("no reminder to " + what)
		return false
	}
	return true
}

func (m *Model) reminderPick(labels func() []string) field {
	return field{Label: "reminder", Keep: true, Options: labels}
}

func (m *Model) validReminder(v string) (int, string) {
	if m.reminderOf(v) == nil {
		return 0, "pick a reminder from the list"
	}
	return 0, ""
}

func (m *Model) openReminderEdit() tea.Cmd {
	all := func() []string { return m.reminderLabels(nil) }
	if !m.pickReminder(all(), "edit") {
		return nil
	}
	m.mode = modeForm
	m.form = &form{
		Title: "edit reminder",
		Fields: []field{
			m.reminderPick(all),
			{Label: "title", Keep: true, Hint: "empty keeps it"},
			{Label: "message", Keep: true, Hint: "empty keeps it; - clears it"},
			{Label: "every", Keep: true, Hint: "empty keeps it; a new interval restarts the wait"},
		},
		Validate: func(v []string) (int, string) {
			if f, msg := m.validReminder(v[0]); msg != "" {
				return f, msg
			}
			if v[3] != "" {
				if _, err := store.ParseInterval(v[3]); err != nil {
					return 3, err.Error()
				}
			}
			return 0, ""
		},
		Done: func(v []string) tea.Cmd {
			return func() tea.Msg {
				r := m.reminderOf(v[0])
				if r == nil {
					return actionMsg{err: fmt.Errorf("reminder is gone")}
				}
				var e store.ReminderEdit
				if v[1] != "" {
					e.Title = &v[1]
				}
				if v[2] != "" {
					msg := v[2]
					if msg == "-" {
						msg = ""
					}
					e.Message = &msg
				}
				if v[3] != "" {
					sec, _ := store.ParseInterval(v[3])
					e.Interval = &sec
				}
				out, err := m.store.EditReminder(r.ID, e)
				if err != nil {
					return actionMsg{err: err}
				}
				return actionMsg{status: "edited reminder " + out.Title}
			}
		},
	}
	return nil
}

func (m *Model) openReminderSwitch(on bool) tea.Cmd {
	verb := "disable"
	if on {
		verb = "enable"
	}
	labels := func() []string { return m.reminderLabels(func(r store.Reminder) bool { return r.Enabled != on }) }
	if !m.pickReminder(labels(), verb) {
		return nil
	}
	m.mode = modeForm
	m.form = &form{
		Title:    verb + " reminder",
		Fields:   []field{m.reminderPick(labels)},
		Validate: func(v []string) (int, string) { return m.validReminder(v[0]) },
		Done: func(v []string) tea.Cmd {
			return func() tea.Msg {
				r := m.reminderOf(v[0])
				if r == nil {
					return actionMsg{err: fmt.Errorf("reminder is gone")}
				}
				out, err := m.store.EditReminder(r.ID, store.ReminderEdit{Enabled: &on})
				if err != nil {
					return actionMsg{err: err}
				}
				return actionMsg{status: verb + "d reminder " + out.Title}
			}
		},
	}
	return nil
}

func (m *Model) openReminderDelete() tea.Cmd {
	all := func() []string { return m.reminderLabels(nil) }
	if !m.pickReminder(all(), "delete") {
		return nil
	}
	m.mode = modeForm
	m.form = &form{
		Title:    "delete reminder",
		Fields:   []field{m.reminderPick(all)},
		Validate: func(v []string) (int, string) { return m.validReminder(v[0]) },
		Done: func(v []string) tea.Cmd {
			return func() tea.Msg {
				r := m.reminderOf(v[0])
				if r == nil {
					return actionMsg{err: fmt.Errorf("reminder is gone")}
				}
				if err := m.store.DeleteReminder(r.ID); err != nil {
					return actionMsg{err: err}
				}
				return actionMsg{status: "deleted reminder " + r.Title}
			}
		},
	}
	return nil
}
