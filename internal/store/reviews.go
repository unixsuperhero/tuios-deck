package store

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// Review is a completed turn read out of history: a prompt the harness
// reported (a prompt-submitted entry from agent-activity) paired with the
// next response-completed entry of the same window. Nothing but the mark is
// stored; the list is computed on every call. StartedAt and CompletedAt are
// nanoseconds, as the front ends format them; ReviewedAt is milliseconds.
type Review struct {
	ID          string `json:"id"`
	Session     string `json:"session"`
	Window      string `json:"window"`
	Pane        string `json:"pane"`
	Harness     string `json:"harness"`
	Prompt      string `json:"prompt"`
	Response    string `json:"response"`
	Output      string `json:"output,omitempty"`
	StartedAt   int64  `json:"started_at"`
	CompletedAt int64  `json:"completed_at"`
	ReviewedAt  int64  `json:"reviewed_at"`
	// Until is when the window's next prompt was reported, in nanoseconds,
	// 0 when none has been: a capture of this turn was taken before it.
	Until int64 `json:"-"`
}

// TurnEndPrefix opens the detail history writes for a response-completed
// entry; the harness's turn-end text follows it, or TurnEndEmpty when the
// harness reported none.
const (
	TurnEndPrefix = "Turn-end hook report (may be a summary, not the full response):\n\n"
	TurnEndEmpty  = "The harness reported completion without response text."
)

// Reviews pairs history's turns, oldest completion first with a stable
// tie-break. Per window, a prompt-submitted entry from agent-activity is
// pending until the next response-completed entry of that window pairs with
// it; a session_start, or another prompt, in between drops the pending
// prompt. A prompt the deck sent (source "deck") is not a turn until the
// harness reports it. Pane and Harness are left for the caller to fill from
// a live listing.
func (s *Store) Reviews() []Review {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.reviews()
}

func (s *Store) reviews() []Review {
	var turns []*HistoryEntry
	for i := range s.history {
		e := &s.history[i]
		if e.Source != "agent-activity" {
			continue
		}
		switch e.Kind {
		case "prompt-submitted", "response-completed", "session_start":
			turns = append(turns, e)
		}
	}
	// On one millisecond, a response closes the turn before a new prompt
	// opens the next one.
	rank := map[string]int{"session_start": 0, "response-completed": 1, "prompt-submitted": 2}
	sort.SliceStable(turns, func(i, j int) bool {
		if turns[i].At != turns[j].At {
			return turns[i].At < turns[j].At
		}
		return rank[turns[i].Kind] < rank[turns[j].Kind]
	})
	ms := int64(time.Millisecond)
	pending := map[string]*HistoryEntry{}
	last := map[string]int{} // window → index in out of its latest review
	var out []Review
	for _, e := range turns {
		switch e.Kind {
		case "session_start":
			delete(pending, e.Window)
		case "prompt-submitted":
			pending[e.Window] = e
			if i, ok := last[e.Window]; ok && out[i].Until == 0 {
				out[i].Until = e.At * ms
			}
		case "response-completed":
			p := pending[e.Window]
			if p == nil {
				continue
			}
			delete(pending, e.Window)
			if p.Detail == "" {
				continue
			}
			response := strings.TrimPrefix(e.Detail, TurnEndPrefix)
			if response == TurnEndEmpty {
				response = ""
			}
			out = append(out, Review{ID: e.ID, Session: e.Session, Window: e.Window,
				Prompt: p.Detail, Response: response,
				StartedAt: p.At * ms, CompletedAt: e.At * ms, ReviewedAt: s.marks[e.ID]})
			last[e.Window] = len(out) - 1
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].CompletedAt == out[j].CompletedAt {
			return out[i].ID < out[j].ID
		}
		return out[i].CompletedAt < out[j].CompletedAt
	})
	return out
}

// OpenReview marks the review with id reviewed, once: the first open stamps
// it and later opens keep the stamp. It returns the review without output;
// the caller attaches the turn's capture.
func (s *Store) OpenReview(id string) (Review, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, r := range s.reviews() {
		if r.ID != id {
			continue
		}
		if r.ReviewedAt == 0 {
			r.ReviewedAt = time.Now().UnixMilli()
			s.marks[id] = r.ReviewedAt
			if err := s.save(); err != nil {
				delete(s.marks, id)
				return Review{}, err
			}
		}
		return r, nil
	}
	return Review{}, fmt.Errorf("no review %s", id)
}

// migrateReviews turns the reviewed entries of the retained-review table an
// earlier deck wrote into marks, best effort: an old review's completion
// time was the daemon's nanoseconds, history keeps milliseconds, and a
// response-completed entry of the same window at that millisecond takes the
// mark. The rest of the old table is dropped; it is never written back.
func (s *Store) migrateReviews(old []Review) {
	byTurn := map[string]string{}
	for _, e := range s.history {
		if e.Source == "agent-activity" && e.Kind == "response-completed" {
			byTurn[fmt.Sprintf("%s/%d", e.Window, e.At)] = e.ID
		}
	}
	for _, r := range old {
		if r.ReviewedAt == 0 {
			continue
		}
		id := byTurn[fmt.Sprintf("%s/%d", r.Window, r.CompletedAt/int64(time.Millisecond))]
		if id != "" && s.marks[id] == 0 {
			s.marks[id] = r.ReviewedAt
		}
	}
}

// pruneMarks drops the marks of turns history no longer holds.
func (s *Store) pruneMarks() {
	if len(s.marks) == 0 {
		return
	}
	held := make(map[string]bool, len(s.history))
	for _, e := range s.history {
		held[e.ID] = true
	}
	for id := range s.marks {
		if !held[id] {
			delete(s.marks, id)
		}
	}
}
