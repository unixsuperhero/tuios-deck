package web

import (
	"net/http"

	"github.com/unixsuperhero/tuios-deck/internal/store"
)

// reminderView adds the interval as text, as the page and the forms write it.
type reminderView struct {
	store.Reminder
	Every string `json:"every"`
}

func reminderJSON(r store.Reminder) reminderView {
	return reminderView{Reminder: r, Every: store.FormatInterval(r.Interval)}
}

func (s *Server) listReminders(w http.ResponseWriter, r *http.Request) {
	out := []reminderView{}
	for _, x := range s.Store.Reminders() {
		out = append(out, reminderJSON(x))
	}
	writeJSON(w, 200, map[string]any{"reminders": out})
}

func (s *Server) createReminder(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Title   string `json:"title"`
		Message string `json:"message"`
		Every   string `json:"every"`
	}
	if err := readJSON(r, &in); err != nil {
		badRequest(w, "bad json: "+err.Error())
		return
	}
	if in.Every == "" {
		in.Every = "30m"
	}
	sec, err := store.ParseInterval(in.Every)
	if err != nil {
		badRequest(w, err.Error())
		return
	}
	x, err := s.Store.AddReminder(in.Title, in.Message, sec)
	if err != nil {
		storeErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"status": "reminder " + x.Title + " every " + store.FormatInterval(x.Interval), "reminder": reminderJSON(x)})
}

// patchReminder changes the fields the body names: title, message, every
// and enabled.
func (s *Server) patchReminder(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if s.Store.Reminder(id) == nil {
		writeJSON(w, 404, map[string]any{"error": "no reminder " + id})
		return
	}
	var in struct {
		Title   *string `json:"title"`
		Message *string `json:"message"`
		Every   *string `json:"every"`
		Enabled *bool   `json:"enabled"`
	}
	if err := readJSON(r, &in); err != nil {
		badRequest(w, "bad json: "+err.Error())
		return
	}
	e := store.ReminderEdit{Title: in.Title, Message: in.Message, Enabled: in.Enabled}
	if in.Every != nil {
		sec, err := store.ParseInterval(*in.Every)
		if err != nil {
			badRequest(w, err.Error())
			return
		}
		e.Interval = &sec
	}
	x, err := s.Store.EditReminder(id, e)
	if err != nil {
		storeErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"status": "updated reminder " + x.Title, "reminder": reminderJSON(x)})
}

func (s *Server) deleteReminder(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.Store.DeleteReminder(id); err != nil {
		writeJSON(w, 404, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]any{"status": "deleted " + id})
}
