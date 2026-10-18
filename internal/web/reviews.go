package web

import (
	"net/http"

	"github.com/unixsuperhero/tuios-deck/internal/panes"
	"github.com/unixsuperhero/tuios-deck/internal/store"
)

// listReviews is history's turns, oldest completion first. Nothing is
// collected: ?refresh=1 is accepted for older pages and ignored.
func (s *Server) listReviews(w http.ResponseWriter, r *http.Request) {
	rows, running := s.Store.Reviews()
	warning := s.nameReviews(r, rows)
	writeJSON(w, 200, map[string]any{"reviews": rows, "running": running, "warning": warning})
}

// nameReviews fills pane names from a live listing; the warning is why it
// could not when it could not, and the short ids stand in.
func (s *Server) nameReviews(r *http.Request, rows []store.Review) string {
	ctx, cancel := callCtx(r)
	defer cancel()
	sessions, err := panes.List(ctx, s.Client)
	panes.NameReviews(sessions, rows)
	if err != nil {
		return err.Error()
	}
	return ""
}

func (s *Server) openReview(w http.ResponseWriter, r *http.Request) {
	item, err := s.Store.OpenReview(r.PathValue("id"))
	if err != nil {
		storeErr(w, err)
		return
	}
	rows := []store.Review{item}
	s.nameReviews(r, rows)
	panes.ReviewOutput(s.CaptureDir, &rows[0])
	writeJSON(w, 200, map[string]any{"review": rows[0], "responses_text": store.ResponsesText(rows[0])})
}
