package store

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"sort"
	"strings"
	"time"
)

// HistoryEntry preserves a source observation, not a reconstructed conversation.
// At and Observed are Unix milliseconds; SourceID retains the source's identity.
type HistoryEntry struct {
	ID       string `json:"id"`
	Kind     string `json:"kind"`
	At       int64  `json:"at"`
	Observed int64  `json:"observed"`
	Source   string `json:"source"`
	SourceID string `json:"source_id"`
	Session  string `json:"session,omitempty"`
	Window   string `json:"window,omitempty"`
	Summary  string `json:"summary"`
	Detail   string `json:"detail"`
}

// maxHistory bounds the timeline the deck keeps, in memory and in the
// database; Reviews walks all of it on every call. The oldest entries go
// first.
const maxHistory = 10000

// droppedHistoryKinds were recorded by earlier decks and are pruned on load.
// Harnesses animate pane titles (a spinner frame per event), so retitles were
// 98% of one store's history and carried nothing a person asked to keep.
var droppedHistoryKinds = map[string]bool{"window-retitled": true}

// pruneHistory drops the kinds no longer recorded and caps the rest.
func pruneHistory(h []HistoryEntry) []HistoryEntry {
	kept := h[:0:0]
	for _, e := range h {
		if !droppedHistoryKinds[e.Kind] {
			kept = append(kept, e)
		}
	}
	if len(kept) > maxHistory {
		kept = kept[len(kept)-maxHistory:]
	}
	return kept
}

// AddHistory atomically keeps new observations. Re-reading a retained source
// never duplicates an entry or changes its first observation. Callers batch
// entries: one call is one transaction that inserts the new entries and
// deletes the ones the cap pushes out.
func (s *Store) AddHistory(entries ...HistoryEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.historyIDs == nil {
		s.historyIDs = make(map[string]bool, len(s.history))
		for _, e := range s.history {
			s.historyIDs[e.ID] = true
		}
	}
	before := s.history
	var added []HistoryEntry
	for _, e := range entries {
		if s.historyIDs[e.ID] {
			continue
		}
		if e.Observed == 0 {
			e.Observed = time.Now().UnixMilli()
		}
		added = append(added, e)
		s.historyIDs[e.ID] = true
	}
	if len(added) == 0 {
		return nil
	}
	next := append(append(make([]HistoryEntry, 0, len(s.history)+len(added)), s.history...), added...)
	var dropped []HistoryEntry
	if n := len(next) - maxHistory; n > 0 {
		dropped = next[:n]
		next = next[n:]
	}
	err := s.inTx(func(tx *sql.Tx) error {
		if err := insertHistory(tx, added); err != nil {
			return err
		}
		return deleteHistory(tx, dropped)
	})
	if err != nil {
		s.history = before
		s.historyIDs = nil
		return err
	}
	for _, e := range dropped {
		delete(s.historyIDs, e.ID)
	}
	s.history = next
	return nil
}

// History returns newest first, with a deterministic tie-break for equal times.
func (s *Store) History() ([]HistoryEntry, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := append([]HistoryEntry{}, s.history...)
	sort.Slice(out, func(i, j int) bool {
		if out[i].At != out[j].At {
			return out[i].At > out[j].At
		}
		order := func(kind string) int {
			switch kind {
			case "command-completed":
				return 2
			case "command-submitted":
				return 1
			}
			return 0
		}
		if a, b := order(out[i].Kind), order(out[j].Kind); a != b {
			return a > b
		}
		return out[i].ID > out[j].ID
	})
	return out, s.historyError
}

// LastActivity is the newest history time (Unix ms) per window: what the
// stale mark reads for a pane's last activity.
func (s *Store) LastActivity() map[string]int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[string]int64{}
	for _, e := range s.history {
		if e.Window != "" && e.At > out[e.Window] {
			out[e.Window] = e.At
		}
	}
	return out
}

func (s *Store) SetHistoryError(message string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.historyError = message
}

// RecordSubmission records a successful daemon acknowledgement, not execution.
// A history write failure is a warning: the caller must not retry already sent input.
func (s *Store) RecordSubmission(session, window, text, outcome string) string {
	if text == "" {
		return ""
	}
	kind, label := "prompt-submitted", "Prompt sent"
	switch outcome {
	case "queued":
		kind, label = "prompt-queued", "Prompt accepted by daemon queue (delivery not confirmed)"
	case "terminal":
		kind, label = "input-submitted", "Terminal input sent (execution not confirmed)"
	case "sent":
	default:
		kind, label = "prompt-status", "Prompt status: "+outcome
	}
	var token [16]byte
	if _, err := rand.Read(token[:]); err != nil {
		return "; history not saved: " + err.Error()
	}
	id := hex.EncodeToString(token[:])
	first, _, _ := strings.Cut(text, "\n")
	err := s.AddHistory(HistoryEntry{ID: "deck/" + id, Source: "deck", SourceID: id, Kind: kind, At: time.Now().UnixMilli(), Session: session, Window: window, Summary: label + ": " + first, Detail: label + "\n\n" + text})
	if err != nil {
		return "; input was accepted, but history was not saved: " + err.Error()
	}
	return ""
}
