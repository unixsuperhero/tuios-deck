package store

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Reminder repeats every Interval seconds while Enabled. Next, a unix
// time, is when it is due; it is stored so a restart keeps the schedule.
type Reminder struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Message  string `json:"message"`
	Interval int64  `json:"interval"`
	Enabled  bool   `json:"enabled"`
	Next     int64  `json:"next"`
	Last     int64  `json:"last"`
	Created  int64  `json:"created"`
}

// MinInterval is the shortest repeat, in seconds.
const MinInterval = 1

var dayPrefix = regexp.MustCompile(`^(\d+(?:\.\d+)?)d(.*)$`)

// ParseInterval reads "90s", "30m", "2h", "1d" or a Go duration such as
// "1h30m". A bare number is minutes.
func ParseInterval(s string) (int64, error) {
	s = strings.TrimSpace(strings.ToLower(s))
	orig := s
	if s == "" {
		return 0, errors.New("interval is empty")
	}
	if _, err := strconv.ParseInt(s, 10, 64); err == nil {
		s += "m"
	}
	// Days are not a Go unit: take a leading "Nd" apart, so what
	// FormatInterval writes ("1d2h3m") reads back.
	var d time.Duration
	if m := dayPrefix.FindStringSubmatch(s); m != nil {
		n, _ := strconv.ParseFloat(m[1], 64)
		d = time.Duration(n * float64(24*time.Hour))
		s = m[2]
	}
	if s != "" {
		rest, err := time.ParseDuration(s)
		if err != nil {
			return 0, fmt.Errorf("interval %q is not a duration like 30m, 2h or 1d", orig)
		}
		d += rest
	}
	if d < MinInterval*time.Second {
		return 0, errors.New("interval must be at least 1s")
	}
	return int64(d / time.Second), nil
}

// FormatInterval is seconds as the shortest "1d2h3m4s" style text.
func FormatInterval(sec int64) string {
	var b strings.Builder
	for _, u := range []struct {
		n int64
		s string
	}{{86400, "d"}, {3600, "h"}, {60, "m"}, {1, "s"}} {
		if sec >= u.n {
			fmt.Fprintf(&b, "%d%s", sec/u.n, u.s)
			sec %= u.n
		}
	}
	if b.Len() == 0 {
		return "0s"
	}
	return b.String()
}

func (s *Store) reminderIndex(id string) int {
	for i, r := range s.reminders {
		if r.ID == id {
			return i
		}
	}
	return -1
}

// AddReminder stores an enabled reminder first due one interval from now.
func (s *Store) AddReminder(title, message string, interval int64) (Reminder, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	title = strings.TrimSpace(title)
	if title == "" {
		return Reminder{}, errors.New("reminder title is empty")
	}
	if interval < MinInterval {
		return Reminder{}, errors.New("interval must be at least 1s")
	}
	t := now()
	r := Reminder{ID: s.newID("r_"), Title: title, Message: message, Interval: interval, Enabled: true, Next: t + interval, Created: t}
	s.reminders = append(s.reminders, r)
	return r, s.save()
}

// Reminders is every reminder in the order added.
func (s *Store) Reminders() []Reminder {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Reminder{}, s.reminders...)
}

// Reminder is the reminder with that id, or nil.
func (s *Store) Reminder(id string) *Reminder {
	s.mu.Lock()
	defer s.mu.Unlock()
	if i := s.reminderIndex(id); i >= 0 {
		r := s.reminders[i]
		return &r
	}
	return nil
}

// ReminderEdit holds the fields to change; nil leaves one as it was. A new
// interval or turning a reminder on restarts its wait from now.
type ReminderEdit struct {
	Title    *string
	Message  *string
	Interval *int64
	Enabled  *bool
}

// EditReminder applies the edit and saves.
func (s *Store) EditReminder(id string, e ReminderEdit) (Reminder, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	i := s.reminderIndex(id)
	if i < 0 {
		return Reminder{}, fmt.Errorf("no reminder %s", id)
	}
	r := s.reminders[i]
	if e.Title != nil {
		t := strings.TrimSpace(*e.Title)
		if t == "" {
			return Reminder{}, errors.New("reminder title is empty")
		}
		r.Title = t
	}
	if e.Message != nil {
		r.Message = *e.Message
	}
	restart := false
	if e.Interval != nil {
		if *e.Interval < MinInterval {
			return Reminder{}, errors.New("interval must be at least 1s")
		}
		r.Interval, restart = *e.Interval, true
	}
	if e.Enabled != nil {
		restart = restart || (*e.Enabled && !r.Enabled)
		r.Enabled = *e.Enabled
	}
	if restart {
		r.Next = now() + r.Interval
	}
	s.reminders[i] = r
	return r, s.save()
}

// DeleteReminder removes the reminder.
func (s *Store) DeleteReminder(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	i := s.reminderIndex(id)
	if i < 0 {
		return fmt.Errorf("no reminder %s", id)
	}
	s.reminders = append(s.reminders[:i], s.reminders[i+1:]...)
	return s.save()
}

// TakeDue returns the enabled reminders due at t and moves each one's
// Next to the first slot after t on its schedule, saved before it
// returns: a reminder missed while the deck was off fires once, not once
// per missed interval, and a crash after the save loses a delivery rather
// than repeating it.
func (s *Store) TakeDue(t int64) ([]Reminder, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var due []Reminder
	for i, r := range s.reminders {
		if !r.Enabled || r.Next > t {
			continue
		}
		due = append(due, r)
		r.Last = t
		r.Next += ((t-r.Next)/r.Interval + 1) * r.Interval
		s.reminders[i] = r
	}
	if due == nil {
		return nil, nil
	}
	return due, s.save()
}
