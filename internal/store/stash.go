package store

import (
	"crypto/rand"
	"strings"
)

type StashedPrompt struct {
	DraftID string `json:"draft_id,omitempty"`
	Text    string `json:"text"`
	Saved   int64  `json:"saved"`
}

// StashedPrompts returns newest first, independently of the store's slice.
func (s *Store) StashedPrompts() []StashedPrompt {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]StashedPrompt, 0, len(s.stashes))
	seen := make(map[string]bool, len(s.stashes))
	for _, p := range s.stashes {
		if !seen[p.Text] {
			out = append(out, p)
			seen[p.Text] = true
		}
	}
	return out
}

// StashPrompt keeps exact text before the composer removes it. A failed write
// leaves both the on-disk store and its in-memory stash unchanged.
func (s *Store) StashPrompt(text string) error {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.stashes) > 0 && s.stashes[0].Text == text && s.stashes[0].DraftID == "" {
		return nil
	}
	previous := s.stashes
	next := make([]StashedPrompt, 1, len(previous)+1)
	next[0] = StashedPrompt{Text: text, Saved: now()}
	for _, p := range previous {
		if p.Text != text {
			next = append(next, p)
		}
	}
	s.stashes = next
	if err := s.save(); err != nil {
		s.stashes = previous
		return err
	}
	return nil
}

// SavePromptDraft updates one composer without accumulating an entry per key.
// Emptying a composer keeps its last nonempty text recoverable.
func (s *Store) SavePromptDraft(id, text string) (string, error) {
	if strings.TrimSpace(text) == "" {
		return "", nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if id == "" {
		id = rand.Text()
	}
	previous := s.stashes
	next := make([]StashedPrompt, 1, len(previous)+1)
	next[0] = StashedPrompt{DraftID: id, Text: text, Saved: now()}
	for _, p := range previous {
		if p.DraftID != id {
			next = append(next, p)
		}
	}
	s.stashes = next
	if err := s.save(); err != nil {
		s.stashes = previous
		return "", err
	}
	return id, nil
}
