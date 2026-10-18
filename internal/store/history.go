package store

import (
	"crypto/rand"
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

// AddHistory atomically keeps new observations. Re-reading a retained source
// never duplicates an entry or changes its first observation.
func (s *Store) AddHistory(entries ...HistoryEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.historyIDs == nil {
		s.historyIDs = make(map[string]bool, len(s.history))
		for _, e := range s.history {
			s.historyIDs[e.ID] = true
		}
	}
	old := len(s.history)
	for _, e := range entries {
		if s.historyIDs[e.ID] {
			continue
		}
		if e.Observed == 0 {
			e.Observed = time.Now().UnixMilli()
		}
		s.history = append(s.history, e)
		s.historyIDs[e.ID] = true
	}
	if len(s.history) == old {
		return nil
	}
	if err := s.save(); err != nil {
		for _, e := range s.history[old:] {
			delete(s.historyIDs, e.ID)
		}
		s.history = s.history[:old]
		return err
	}
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
