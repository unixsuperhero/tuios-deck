package store

import (
	"errors"
	"fmt"
	"sort"
)

// ContextItem is something worth keeping that was found in a pane: a link,
// a file or a folder, a pull request, a prompt the person gave an agent or
// a command a shell ran. Session, Window and Pane say where it was found.
// A task implies its project, so ProjectID is set whenever TaskID is.
type ContextItem struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	Text      string `json:"text"`
	Session   string `json:"session"`
	Window    string `json:"window"`
	Pane      string `json:"pane"`
	ProjectID string `json:"project_id"`
	TaskID    string `json:"task_id"`
	Starred   bool   `json:"starred"`
	Pinned    bool   `json:"pinned"`
	Created   int64  `json:"created"`
}

// ContextKinds are the kinds a context item may have.
func ContextKinds() []string {
	return []string{"link", "file", "dir", "pr", "prompt", "command"}
}

func validKind(k string) bool {
	for _, c := range ContextKinds() {
		if c == k {
			return true
		}
	}
	return false
}

func (s *Store) contextIndex(id string) int {
	for i, c := range s.contexts {
		if c.ID == id {
			return i
		}
	}
	return -1
}

// AddContextItems stores the items that are new: an item with the kind,
// text and window of one already stored is skipped. It reports how many
// were added. Each new item gets an id and, when it has none, a created
// time.
func (s *Store) AddContextItems(items []ContextItem) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	seen := map[string]bool{}
	for _, c := range s.contexts {
		seen[c.Kind+"\x00"+c.Text+"\x00"+c.Window] = true
	}
	added := 0
	for _, it := range items {
		if !validKind(it.Kind) {
			return added, fmt.Errorf("context kind %q is not one of link file dir pr prompt command", it.Kind)
		}
		if it.Text == "" {
			continue
		}
		key := it.Kind + "\x00" + it.Text + "\x00" + it.Window
		if seen[key] {
			continue
		}
		seen[key] = true
		it.ID = s.newID("c_")
		if it.Created == 0 {
			it.Created = now()
		}
		s.contexts = append(s.contexts, it)
		added++
	}
	if added == 0 {
		return 0, nil
	}
	return added, s.save()
}

// SetContextStar stars or unstars an item.
func (s *Store) SetContextStar(id string, on bool) error {
	return s.changeContext(id, func(c *ContextItem) error { c.Starred = on; return nil })
}

// SetContextPin pins or unpins an item.
func (s *Store) SetContextPin(id string, on bool) error {
	return s.changeContext(id, func(c *ContextItem) error { c.Pinned = on; return nil })
}

// AssignContext ties an item to a task, which brings its project, or to a
// project alone; both empty clears it. Giving both is refused, as it is
// for panes.
func (s *Store) AssignContext(id, taskID, projectID string) error {
	return s.changeContext(id, func(c *ContextItem) error {
		switch {
		case taskID != "" && projectID != "":
			return errors.New("assign to a task or a project, not both")
		case taskID != "":
			if s.taskIndex(taskID) < 0 {
				return fmt.Errorf("no task %s", taskID)
			}
			c.TaskID, c.ProjectID = taskID, s.projectOf(taskID)
		case projectID != "":
			if s.projectIndex(projectID) < 0 {
				return fmt.Errorf("no project %s", projectID)
			}
			c.TaskID, c.ProjectID = "", projectID
		default:
			c.TaskID, c.ProjectID = "", ""
		}
		return nil
	})
}

// DeleteContextItem removes an item.
func (s *Store) DeleteContextItem(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	i := s.contextIndex(id)
	if i < 0 {
		return fmt.Errorf("no context item %s", id)
	}
	s.contexts = append(s.contexts[:i], s.contexts[i+1:]...)
	return s.save()
}

func (s *Store) changeContext(id string, change func(c *ContextItem) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	i := s.contextIndex(id)
	if i < 0 {
		return fmt.Errorf("no context item %s", id)
	}
	c := s.contexts[i]
	if err := change(&c); err != nil {
		return err
	}
	s.contexts[i] = c
	return s.save()
}

// ContextItems is every item, newest first.
func (s *Store) ContextItems() []ContextItem {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]ContextItem, len(s.contexts))
	copy(out, s.contexts)
	// Items added together share a created second; the later one in the
	// table is the newer.
	idx := map[string]int{}
	for i, c := range s.contexts {
		idx[c.ID] = i
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Created != out[j].Created {
			return out[i].Created > out[j].Created
		}
		return idx[out[i].ID] > idx[out[j].ID]
	})
	return out
}

// ContextItem is the item with that id, or nil.
func (s *Store) ContextItem(id string) *ContextItem {
	s.mu.Lock()
	defer s.mu.Unlock()
	if i := s.contextIndex(id); i >= 0 {
		c := s.contexts[i]
		return &c
	}
	return nil
}
