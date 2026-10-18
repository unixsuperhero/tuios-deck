package store

import (
	"fmt"
	"sort"
	"time"
)

// Review is a completed prompt, retained even after its pane or activity expires.
// Activity timestamps are nanoseconds; ReviewedAt is milliseconds for the UI.
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
}

// AddReviews saves only unseen completed turns. Existing snapshots and review
// timestamps are immutable, including when the daemon repeats its activity ring.
func (s *Store) AddReviews(items []Review) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	known := make(map[string]bool, len(s.reviews))
	for _, r := range s.reviews {
		known[r.ID] = true
	}
	old := len(s.reviews)
	for _, r := range items {
		if !known[r.ID] && r.ID != "" && r.Prompt != "" && r.CompletedAt >= r.StartedAt && r.CompletedAt > 0 {
			r.ReviewedAt = 0
			s.reviews = append(s.reviews, r)
			known[r.ID] = true
		}
	}
	if len(s.reviews) == old {
		return nil
	}
	if err := s.save(); err != nil {
		s.reviews = s.reviews[:old]
		return err
	}
	return nil
}

// Reviews returns completion order, oldest first, with a stable tie-breaker.
func (s *Store) Reviews() []Review {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := append([]Review{}, s.reviews...)
	sort.Slice(out, func(i, j int) bool {
		if out[i].CompletedAt == out[j].CompletedAt {
			return out[i].ID < out[j].ID
		}
		return out[i].CompletedAt < out[j].CompletedAt
	})
	return out
}

// OpenReview marks only the explicitly opened item, once, before returning it.
func (s *Store) OpenReview(id string) (Review, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.reviews {
		r := &s.reviews[i]
		if r.ID != id {
			continue
		}
		if r.ReviewedAt == 0 {
			r.ReviewedAt = time.Now().UnixMilli()
			if err := s.save(); err != nil {
				r.ReviewedAt = 0
				return Review{}, err
			}
		}
		return *r, nil
	}
	return Review{}, fmt.Errorf("no review %s", id)
}
