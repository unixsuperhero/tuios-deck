package web

import (
	"net/http"
	"strings"

	"github.com/unixsuperhero/tuios-deck/internal/history"
	"github.com/unixsuperhero/tuios-deck/internal/store"
)

func (s *Server) history(w http.ResponseWriter, r *http.Request) {
	entries, observationError := s.Store.History()
	q := r.URL.Query()
	query := strings.ToLower(q.Get("q"))
	filtered := make([]store.HistoryEntry, 0, len(entries))
	for _, e := range entries {
		if kind := q.Get("kind"); kind != "" && e.Kind != kind {
			continue
		}
		if window := q.Get("window"); window != "" && e.Window != window {
			continue
		}
		if query != "" && !strings.Contains(strings.ToLower(e.Summary+"\n"+e.Detail+"\n"+e.Session+"\n"+e.Window), query) {
			continue
		}
		filtered = append(filtered, e)
	}
	writeJSON(w, 200, map[string]any{"entries": filtered, "boundary": history.Boundary, "observation_error": observationError})
}
