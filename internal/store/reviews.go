package store

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// Review is a turn read out of history: a prompt the person gave, which the
// harness reported as a prompt-submitted entry (source agent-activity), and
// every response-completed entry of the same window until the person's next
// prompt. A turn has more than one response when a finished subagent woke
// it: the harness reports that wake-up as a prompt too, with the text of
// NotificationPrefix, and it continues the turn rather than opening one.
// Nothing but the mark is stored; the list is computed on every call.
// StartedAt and CompletedAt are nanoseconds, as the front ends format them;
// ReviewedAt is milliseconds.
type Review struct {
	ID       string `json:"id"` // the prompt-submitted entry's history id
	Session  string `json:"session"`
	Window   string `json:"window"`
	Pane     string `json:"pane"`
	Harness  string `json:"harness"`
	Prompt   string `json:"prompt"`
	Response string `json:"response"` // the last response's text
	// Responses is every response of the turn, oldest first; all but the
	// last are interim, given while subagents were still reporting back.
	Responses   []ReviewResponse `json:"responses"`
	Output      string           `json:"output,omitempty"`
	StartedAt   int64            `json:"started_at"`
	CompletedAt int64            `json:"completed_at"` // the last response
	ReviewedAt  int64            `json:"reviewed_at"`
	// Until is when the window's next prompt was reported, in nanoseconds,
	// 0 when none has been: a capture of this turn was taken before it.
	Until int64 `json:"-"`
}

// ReviewResponse is one response-completed entry of a turn.
type ReviewResponse struct {
	ID   string `json:"id"`
	At   int64  `json:"at"` // nanoseconds
	Text string `json:"text"`
}

// TurnEndPrefix opens the detail history writes for a response-completed
// entry; the harness's turn-end text follows it, or TurnEndEmpty when the
// harness reported none.
const (
	TurnEndPrefix = "Turn-end hook report (may be a summary, not the full response):\n\n"
	TurnEndEmpty  = "The harness reported completion without response text."
)

// NotificationPrefix opens the prompt a harness reports when a finished
// subagent wakes the turn rather than the person (Claude Code's task
// notifications). Such a prompt continues the window's turn.
const NotificationPrefix = "<task-notification>"

// IsNotification says whether a reported prompt is a subagent's wake-up,
// not the person's.
func IsNotification(prompt string) bool {
	return strings.HasPrefix(strings.TrimSpace(prompt), NotificationPrefix)
}

// Reviews is history's finished turns, oldest completion first with a
// stable tie-break, and how many turns with a response are still running
// and so not listed yet. Per window, the person's prompt opens a turn; each
// response-completed entry adds a response; a notification prompt keeps the
// turn open for the next response; the person's next prompt, or a
// session_start, closes it. A turn is finished once it has a response, is
// not waiting for one, and no subagent the window started is still running
// (subagent_start entries without their subagent_stop). A prompt the deck
// sent (source "deck") is not a turn until the harness reports it. Pane and
// Harness are left for the caller to fill from a live listing.
func (s *Store) Reviews() ([]Review, int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.reviews()
}

// window is the pairing state of one window while reviews walks history.
type reviewWindow struct {
	cur      *Review
	awaiting bool // a prompt or a notification waits for its response
	subs     int  // subagents started and not stopped, across turns
}

func (s *Store) reviews() ([]Review, int) {
	var turns []*HistoryEntry
	for i := range s.history {
		e := &s.history[i]
		if e.Source != "agent-activity" {
			continue
		}
		switch e.Kind {
		case "prompt-submitted", "response-completed", "session_start", "subagent_start", "subagent_stop":
			turns = append(turns, e)
		}
	}
	// On one millisecond, a response closes the turn before a new prompt
	// opens the next one, and a subagent's stop comes before the wake-up it
	// caused.
	rank := map[string]int{"session_start": 0, "subagent_stop": 1, "response-completed": 1, "prompt-submitted": 2, "subagent_start": 3}
	sort.SliceStable(turns, func(i, j int) bool {
		if turns[i].At != turns[j].At {
			return turns[i].At < turns[j].At
		}
		return rank[turns[i].Kind] < rank[turns[j].Kind]
	})
	ms := int64(time.Millisecond)
	windows := map[string]*reviewWindow{}
	var out []Review
	// close ends a window's current turn: listed when it has a response.
	close := func(w *reviewWindow) {
		if w.cur != nil && len(w.cur.Responses) > 0 {
			out = append(out, *w.cur)
		}
		w.cur, w.awaiting = nil, false
	}
	for _, e := range turns {
		w := windows[e.Window]
		if w == nil {
			w = &reviewWindow{}
			windows[e.Window] = w
		}
		switch e.Kind {
		case "session_start":
			close(w)
			w.subs = 0
		case "subagent_start":
			w.subs++
		case "subagent_stop":
			w.subs = max(0, w.subs-1)
		case "prompt-submitted":
			if IsNotification(e.Detail) {
				if w.cur != nil {
					w.awaiting = true
				}
				continue
			}
			if w.cur != nil {
				w.cur.Until = e.At * ms
			}
			close(w)
			if e.Detail == "" {
				continue
			}
			w.cur = &Review{ID: e.ID, Session: e.Session, Window: e.Window, Prompt: e.Detail, StartedAt: e.At * ms}
			w.awaiting = true
		case "response-completed":
			if w.cur == nil {
				continue
			}
			text := strings.TrimPrefix(e.Detail, TurnEndPrefix)
			if text == TurnEndEmpty {
				text = ""
			}
			w.cur.Responses = append(w.cur.Responses, ReviewResponse{ID: e.ID, At: e.At * ms, Text: text})
			w.cur.Response, w.cur.CompletedAt = text, e.At*ms
			w.awaiting = false
		}
	}
	running := 0
	for _, w := range windows {
		if w.cur == nil || len(w.cur.Responses) == 0 {
			continue
		}
		if w.awaiting || w.subs > 0 {
			running++
			continue
		}
		close(w)
	}
	for i := range out {
		s.adoptMarks(&out[i])
		out[i].ReviewedAt = s.marks[out[i].ID]
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].CompletedAt == out[j].CompletedAt {
			return out[i].ID < out[j].ID
		}
		return out[i].CompletedAt < out[j].CompletedAt
	})
	return out, running
}

// adoptMarks moves a mark an earlier deck keyed by a response's history id
// onto the turn, which is keyed by its prompt's. The move is written with
// the next save.
func (s *Store) adoptMarks(r *Review) {
	for _, resp := range r.Responses {
		at, ok := s.marks[resp.ID]
		if !ok {
			continue
		}
		if s.marks[r.ID] == 0 || at < s.marks[r.ID] {
			s.marks[r.ID] = at
		}
		delete(s.marks, resp.ID)
	}
}

// ResponsesText is the detail's RESPONSE section: the one response, or
// every response of a turn that subagents woke, each under a heading with
// its time, the last one marked as such.
func ResponsesText(r Review) string {
	const none = "No response text was reported by this harness."
	if len(r.Responses) <= 1 {
		if r.Response == "" {
			return "RESPONSE\n" + none
		}
		return "RESPONSE\n" + r.Response
	}
	n := len(r.Responses)
	var b strings.Builder
	fmt.Fprintf(&b, "RESPONSES (%d: %d interim, each before a subagent reported back, then the last)", n, n-1)
	for i, resp := range r.Responses {
		kind := "interim"
		if i == n-1 {
			kind = "last"
		}
		fmt.Fprintf(&b, "\n\n— %d of %d, %s, %s —\n", i+1, n, kind, time.Unix(0, resp.At).Format("15:04:05"))
		if resp.Text == "" {
			b.WriteString(none)
		} else {
			b.WriteString(resp.Text)
		}
	}
	return b.String()
}

// OpenReview marks the review with id reviewed, once: the first open stamps
// it and later opens keep the stamp. It returns the review without output;
// the caller attaches the turn's capture.
func (s *Store) OpenReview(id string) (Review, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, _ := s.reviews()
	for _, r := range rows {
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
// mark, which reviews then moves onto its turn. The rest of the old table is
// dropped; it is never written back.
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
