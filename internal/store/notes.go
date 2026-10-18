package store

import (
	"fmt"
	"sort"
	"strings"
)

// Note is free text kept on a project or on a task: exactly one of TaskID
// and ProjectID is set. A note stores only its owner; the project of a task
// note is found through the task, so moving a task moves what it sees.
type Note struct {
	ID        string `json:"id"`
	Text      string `json:"text"`
	TaskID    string `json:"task_id"`
	ProjectID string `json:"project_id"`
	Created   int64  `json:"created"`
	Updated   int64  `json:"updated"`
}

// Note relations, as seen from the project or task asking.
const (
	NoteOwn        = "own"
	NoteAncestor   = "ancestor"
	NoteDescendant = "descendant"
)

// VisibleNote is a note a project or task sees, with how it got there and a
// name for its owner. Nothing is copied: it is the owner's note.
type VisibleNote struct {
	Note
	Relation string
	Owner    string
}

func (s *Store) noteIndex(id string) int {
	for i, n := range s.notes {
		if n.ID == id {
			return i
		}
	}
	return -1
}

// AddNote attaches a note to a task or, with no task, to a project; exactly
// one is given and it must exist. The text must not be blank.
func (s *Store) AddNote(taskID, projectID, text string) (Note, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	text = strings.TrimSpace(text)
	if text == "" {
		return Note{}, fmt.Errorf("a note needs text")
	}
	n := Note{Text: text, Created: now()}
	n.Updated = n.Created
	switch {
	case taskID != "" && projectID != "":
		return Note{}, fmt.Errorf("a note belongs to a task or a project, not both")
	case taskID != "":
		if s.taskIndex(taskID) < 0 {
			return Note{}, fmt.Errorf("no task %s", taskID)
		}
		n.TaskID = taskID
	case projectID != "":
		if s.projectIndex(projectID) < 0 {
			return Note{}, fmt.Errorf("no project %s", projectID)
		}
		n.ProjectID = projectID
	default:
		return Note{}, fmt.Errorf("a note needs a task or a project")
	}
	n.ID = s.newID("n_")
	s.notes = append(s.notes, n)
	return n, s.save()
}

// EditNote replaces a note's text.
func (s *Store) EditNote(id, text string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	i := s.noteIndex(id)
	if i < 0 {
		return fmt.Errorf("no note %s", id)
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return fmt.Errorf("a note needs text")
	}
	s.notes[i].Text, s.notes[i].Updated = text, now()
	return s.save()
}

// DeleteNote removes a note.
func (s *Store) DeleteNote(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	i := s.noteIndex(id)
	if i < 0 {
		return fmt.Errorf("no note %s", id)
	}
	s.notes = append(s.notes[:i], s.notes[i+1:]...)
	return s.save()
}

// Note is the note with that id, or nil.
func (s *Store) Note(id string) *Note {
	s.mu.Lock()
	defer s.mu.Unlock()
	if i := s.noteIndex(id); i >= 0 {
		n := s.notes[i]
		return &n
	}
	return nil
}

// Notes is every note, oldest first.
func (s *Store) Notes() []Note {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Note, len(s.notes))
	copy(out, s.notes)
	return out
}

// NotesFor is the notes a task sees (taskID) or, with no task, a project
// sees (projectID): its own, those of its ancestors (the task chain above it
// and its project) and those of its descendants (its subtasks at any depth).
// A project is the ancestor of every task in it and so sees all their notes.
// Siblings and cousins are not related and are not seen. Own notes come
// first, then ancestors from the nearest, then descendants; oldest first
// within each owner.
func (s *Store) NotesFor(taskID, projectID string) []VisibleNote {
	s.mu.Lock()
	defer s.mu.Unlock()
	rank := map[string]int{}   // owner key to order
	rel := map[string]string{} // owner key to relation
	name := map[string]string{}
	add := func(key, relation, label string, order int) {
		if _, ok := rel[key]; !ok {
			rel[key], name[key], rank[key] = relation, label, order
		}
	}
	taskKey := func(id string) string { return "t:" + id }
	projKey := func(id string) string { return "p:" + id }
	projName := func(id string) string {
		if i := s.projectIndex(id); i >= 0 {
			return s.projects[i].Name
		}
		return id
	}
	if taskID != "" {
		chain := s.chain(taskID)
		if len(chain) == 0 {
			return nil
		}
		add(taskKey(taskID), NoteOwn, chain[len(chain)-1].Title, 0)
		order := 1
		for i := len(chain) - 2; i >= 0; i-- {
			add(taskKey(chain[i].ID), NoteAncestor, chain[i].Title, order)
			order++
		}
		pid := chain[0].ProjectID
		add(projKey(pid), NoteAncestor, projName(pid), order)
		desc := map[string]bool{}
		s.collect(taskID, desc)
		for _, t := range s.tasks {
			if desc[t.ID] && t.ID != taskID {
				add(taskKey(t.ID), NoteDescendant, t.Title, 100)
			}
		}
	} else {
		if s.projectIndex(projectID) < 0 {
			return nil
		}
		add(projKey(projectID), NoteOwn, projName(projectID), 0)
		for _, t := range s.tasks {
			if s.projectOf(t.ID) == projectID {
				add(taskKey(t.ID), NoteDescendant, t.Title, 100)
			}
		}
	}
	var out []VisibleNote
	for _, n := range s.notes {
		key := projKey(n.ProjectID)
		if n.TaskID != "" {
			key = taskKey(n.TaskID)
		}
		if r, ok := rel[key]; ok {
			out = append(out, VisibleNote{Note: n, Relation: r, Owner: name[key]})
		}
	}
	idx := map[string]int{}
	for i, n := range s.notes {
		idx[n.ID] = i
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		ka, kb := rank[ownerKey(a.Note)], rank[ownerKey(b.Note)]
		if ka != kb {
			return ka < kb
		}
		return idx[a.ID] < idx[b.ID]
	})
	return out
}

func ownerKey(n Note) string {
	if n.TaskID != "" {
		return "t:" + n.TaskID
	}
	return "p:" + n.ProjectID
}

// removeNotes drops the notes gone says to, for a deleted project or task.
func (s *Store) removeNotes(gone func(Note) bool) {
	kept := s.notes[:0]
	for _, n := range s.notes {
		if !gone(n) {
			kept = append(kept, n)
		}
	}
	s.notes = kept
}
