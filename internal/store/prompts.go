package store

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
)

// Preset is a named way to begin a prompt: Prefix is put in front of the
// prompt text once, and Cwd, when set, is where an agent or a terminal the
// prompt starts is opened.
type Preset struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Prefix  string `json:"prefix"`
	Cwd     string `json:"cwd"`
	Created int64  `json:"created"`
}

// QueuedPrompt is a prompt kept for later. Text is final, its preset's
// prefix already in it, so starting it adds nothing. Window is the pane to
// send it to; with no window, starting it opens Agent in Session at Cwd.
// Like an assignment it names a task or a project: ProjectID is set only
// when there is no task, whose project ProjectOf finds.
type QueuedPrompt struct {
	ID        string `json:"id"`
	Text      string `json:"text"`
	Preset    string `json:"preset"`
	TaskID    string `json:"task_id"`
	ProjectID string `json:"project_id"`
	Cwd       string `json:"cwd"`
	Session   string `json:"session"`
	Agent     string `json:"agent"`
	Window    string `json:"window"`
	Created   int64  `json:"created"`
}

// ApplyPrefix puts the prefix in front of text, a space between them
// unless the prefix ends in whitespace. An empty prefix leaves text as it is.
func ApplyPrefix(prefix, text string) string {
	if prefix == "" {
		return text
	}
	if r := []rune(prefix); !unicode.IsSpace(r[len(r)-1]) {
		prefix += " "
	}
	return prefix + text
}

// ExamplePreset is the short-answer preset the person may ask for; the
// store never adds it by itself.
func ExamplePreset() Preset {
	return Preset{Name: "short answer", Prefix: "Answer this question in less than 10 words:"}
}

func (s *Store) presetIndex(id string) int {
	for i, p := range s.presets {
		if p.ID == id {
			return i
		}
	}
	return -1
}

func (s *Store) presetByName(name string) int {
	for i, p := range s.presets {
		if strings.EqualFold(p.Name, name) {
			return i
		}
	}
	return -1
}

// Presets is every preset in the order they were made.
func (s *Store) Presets() []Preset {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Preset{}, s.presets...)
}

// Preset is the preset with that id, or nil.
func (s *Store) Preset(id string) *Preset {
	s.mu.Lock()
	defer s.mu.Unlock()
	if i := s.presetIndex(id); i >= 0 {
		p := s.presets[i]
		return &p
	}
	return nil
}

// PresetByName is the preset with that name, ignoring case, or nil.
func (s *Store) PresetByName(name string) *Preset {
	s.mu.Lock()
	defer s.mu.Unlock()
	if i := s.presetByName(strings.TrimSpace(name)); i >= 0 {
		p := s.presets[i]
		return &p
	}
	return nil
}

// AddPreset stores a preset. Its name is required and unique.
func (s *Store) AddPreset(name, prefix, cwd string) (Preset, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	name = strings.TrimSpace(name)
	if name == "" {
		return Preset{}, errors.New("a preset needs a name")
	}
	if s.presetByName(name) >= 0 {
		return Preset{}, fmt.Errorf("a preset named %q exists", name)
	}
	p := Preset{ID: s.newID("pp_"), Name: name, Prefix: prefix, Cwd: strings.TrimSpace(cwd), Created: now()}
	s.presets = append(s.presets, p)
	return p, s.save()
}

// UpdatePreset replaces a preset's name, prefix and cwd.
func (s *Store) UpdatePreset(id, name, prefix, cwd string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	i := s.presetIndex(id)
	if i < 0 {
		return fmt.Errorf("no preset %s", id)
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return errors.New("a preset needs a name")
	}
	if j := s.presetByName(name); j >= 0 && j != i {
		return fmt.Errorf("a preset named %q exists", name)
	}
	s.presets[i].Name, s.presets[i].Prefix, s.presets[i].Cwd = name, prefix, strings.TrimSpace(cwd)
	return s.save()
}

// DeletePreset removes a preset. Queued prompts keep their text, which has
// the prefix in it already.
func (s *Store) DeletePreset(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	i := s.presetIndex(id)
	if i < 0 {
		return fmt.Errorf("no preset %s", id)
	}
	s.presets = append(s.presets[:i], s.presets[i+1:]...)
	return s.save()
}

func (s *Store) queueIndex(id string) int {
	for i, q := range s.queue {
		if q.ID == id {
			return i
		}
	}
	return -1
}

// resolveQueueTarget refuses a task and a project together, or one that
// does not exist. Both empty is refused: a queued prompt belongs to a task
// or a project.
func (s *Store) resolveQueueTarget(q *QueuedPrompt) error {
	switch {
	case q.TaskID != "" && q.ProjectID != "":
		return errors.New("queue for a task or a project, not both")
	case q.TaskID != "":
		if s.taskIndex(q.TaskID) < 0 {
			return fmt.Errorf("no task %s", q.TaskID)
		}
	case q.ProjectID != "":
		if s.projectIndex(q.ProjectID) < 0 {
			return fmt.Errorf("no project %s", q.ProjectID)
		}
	default:
		return errors.New("a queued prompt needs a task or a project")
	}
	return nil
}

// QueuePrompt stores a prompt for later and gives it an id.
func (s *Store) QueuePrompt(q QueuedPrompt) (QueuedPrompt, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if strings.TrimSpace(q.Text) == "" {
		return q, errors.New("a queued prompt needs text")
	}
	if err := s.resolveQueueTarget(&q); err != nil {
		return q, err
	}
	q.ID, q.Created = s.newID("q_"), now()
	s.queue = append(s.queue, q)
	return q, s.save()
}

// UpdateQueued replaces everything but the id and the created time.
func (s *Store) UpdateQueued(q QueuedPrompt) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	i := s.queueIndex(q.ID)
	if i < 0 {
		return fmt.Errorf("no queued prompt %s", q.ID)
	}
	if strings.TrimSpace(q.Text) == "" {
		return errors.New("a queued prompt needs text")
	}
	if err := s.resolveQueueTarget(&q); err != nil {
		return err
	}
	q.Created = s.queue[i].Created
	s.queue[i] = q
	return s.save()
}

// DequeuePrompt removes a queued prompt: the one started, or deleted.
func (s *Store) DequeuePrompt(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	i := s.queueIndex(id)
	if i < 0 {
		return fmt.Errorf("no queued prompt %s", id)
	}
	s.queue = append(s.queue[:i], s.queue[i+1:]...)
	return s.save()
}

// Queued is every queued prompt, oldest first.
func (s *Store) Queued() []QueuedPrompt {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]QueuedPrompt{}, s.queue...)
}

// QueuedPrompt is the queued prompt with that id, or nil.
func (s *Store) QueuedPrompt(id string) *QueuedPrompt {
	s.mu.Lock()
	defer s.mu.Unlock()
	if i := s.queueIndex(id); i >= 0 {
		q := s.queue[i]
		return &q
	}
	return nil
}

// dropQueued removes the queued prompts gone reports, the caller holding
// the lock.
func (s *Store) dropQueued(gone func(QueuedPrompt) bool) {
	kept := s.queue[:0]
	for _, q := range s.queue {
		if !gone(q) {
			kept = append(kept, q)
		}
	}
	s.queue = kept
}
