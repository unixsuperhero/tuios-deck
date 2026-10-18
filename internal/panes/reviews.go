package panes

import (
	"time"

	"github.com/unixsuperhero/tuios-deck/internal/capture"
	"github.com/unixsuperhero/tuios-deck/internal/store"
)

// NameReviews fills each review's pane name and harness from a listing.
// History keeps a turn's window id only, so a window the listing no longer
// has is named by its short id, which auto-capture also falls back to.
func NameReviews(sessions []Session, reviews []store.Review) {
	byID := map[string]*Pane{}
	for i := range sessions {
		for j := range sessions[i].Panes {
			p := &sessions[i].Panes[j]
			byID[p.ID] = p
		}
	}
	for i := range reviews {
		r := &reviews[i]
		if p := byID[r.Window]; p != nil {
			r.Pane, r.Harness = p.Name, p.Harness
		} else {
			r.Pane, r.Harness = ShortID(r.Window), ""
		}
	}
}

// ReviewOutput attaches the capture auto-capture took of the review's turn:
// the newest -turn capture of its session and pane (after NameReviews) taken
// at or after the completion and before the window's next prompt. Without
// one, Output stays empty: auto-capture may have been off.
func ReviewOutput(dir string, r *store.Review) {
	var until time.Time
	if r.Until != 0 {
		until = time.Unix(0, r.Until)
	}
	e, ok := capture.Turn(dir, r.Session, r.Pane, time.Unix(0, r.CompletedAt), until)
	if !ok && r.Pane != ShortID(r.Window) {
		e, ok = capture.Turn(dir, r.Session, ShortID(r.Window), time.Unix(0, r.CompletedAt), until)
	}
	if !ok {
		return
	}
	if content, err := capture.Read(dir, e.Name); err == nil {
		r.Output = content
	}
}
