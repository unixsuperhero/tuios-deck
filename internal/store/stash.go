package store

import "strings"

type StashedPrompt struct {
	Text  string `json:"text"`
	Saved int64  `json:"saved"`
}

// StashedPrompts returns newest first, independently of the store's slice.
func (s *Store) StashedPrompts() []StashedPrompt {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]StashedPrompt(nil), s.stashes...)
}

// StashPrompt keeps exact text before the composer removes it. A failed write
// leaves both the on-disk store and its in-memory stash unchanged.
func (s *Store) StashPrompt(text string) error {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.stashes) > 0 && s.stashes[0].Text == text {
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
