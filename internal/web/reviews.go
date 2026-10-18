package web

import (
	"net/http"

	"github.com/unixsuperhero/tuios-deck/internal/review"
)

func (s *Server) listReviews(w http.ResponseWriter, r *http.Request) {
	warning := ""
	if r.URL.Query().Get("refresh") == "1" {
		ctx, cancel := callCtx(r)
		defer cancel()
		if err := review.Collect(ctx, s.Client, s.Store); err != nil {
			warning = err.Error()
		}
	}
	writeJSON(w, 200, map[string]any{"reviews": s.Store.Reviews(), "warning": warning})
}

func (s *Server) openReview(w http.ResponseWriter, r *http.Request) {
	item, err := s.Store.OpenReview(r.PathValue("id"))
	if err != nil {
		storeErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"review": item})
}
