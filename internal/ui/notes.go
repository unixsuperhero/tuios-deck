package ui

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/unixsuperhero/tuios-deck/internal/store"
)

// noteTarget is the project or task the tasks page has selected, as the
// ids a note is stored with, and a name for it. ok is false off the page or
// with nothing selected.
func (m *Model) noteTarget() (taskID, projectID, name string, ok bool) {
	if m.page != pageTasks {
		return "", "", "", false
	}
	r := m.selectedRow()
	if r == nil {
		return "", "", "", false
	}
	if r.Project != nil {
		return "", r.Project.ID, r.Project.Name, true
	}
	return r.Task.ID, "", r.Task.Title, true
}

// ownNoteOptions is the selected row's own notes as "<id> <first line>",
// the entries of the edit and delete forms. Inherited notes belong to
// another row and are changed there.
func (m *Model) ownNoteOptions(taskID, projectID string) []string {
	var out []string
	for _, n := range m.store.NotesFor(taskID, projectID) {
		if n.Relation == store.NoteOwn {
			out = append(out, n.ID+" "+firstLine(n.Text))
		}
	}
	return out
}

// pickNote is the id a note entry names, the entry being one of the own
// notes.
func (m *Model) pickNote(entry, taskID, projectID string) (string, string) {
	id, _, _ := strings.Cut(strings.TrimSpace(entry), " ")
	for _, n := range m.store.NotesFor(taskID, projectID) {
		if n.Relation == store.NoteOwn && n.ID == id {
			return id, ""
		}
	}
	return "", "choose one of the notes listed"
}

func (m *Model) openAddNoteForm() tea.Cmd {
	taskID, projectID, name, ok := m.noteTarget()
	if !ok {
		m.setStatus("select a project or task on the tasks page first")
		return nil
	}
	m.mode = modeForm
	m.form = &form{
		Title:  "add note to " + name,
		Fields: []field{{Label: "note", Keep: true, Multi: true, Hint: "ctrl+s saves"}},
		Validate: func(v []string) (int, string) {
			if strings.TrimSpace(v[0]) == "" {
				return 0, "a note needs text"
			}
			return 0, ""
		},
		Done: func(v []string) tea.Cmd {
			return func() tea.Msg {
				if _, err := m.store.AddNote(taskID, projectID, v[0]); err != nil {
					return actionMsg{err: err}
				}
				return actionMsg{status: "added a note to " + name}
			}
		},
	}
	return nil
}

func (m *Model) openEditNoteForm() tea.Cmd {
	taskID, projectID, name, ok := m.noteTarget()
	if !ok {
		m.setStatus("select a project or task on the tasks page first")
		return nil
	}
	if len(m.ownNoteOptions(taskID, projectID)) == 0 {
		m.setStatus(name + " has no notes of its own to edit")
		return nil
	}
	options := func() []string { return m.ownNoteOptions(taskID, projectID) }
	m.mode = modeForm
	m.form = &form{
		Title: "edit note of " + name,
		Fields: []field{
			{Label: "which", Default: options()[0], Options: options},
			{Label: "text", Keep: true, Multi: true, Hint: "ctrl+s saves; the note's current text is replaced"},
		},
		Validate: func(v []string) (int, string) {
			if _, msg := m.pickNote(v[0], taskID, projectID); msg != "" {
				return 0, msg
			}
			if strings.TrimSpace(v[1]) == "" {
				return 1, "a note needs text"
			}
			return 1, ""
		},
		Done: func(v []string) tea.Cmd {
			id, _ := m.pickNote(v[0], taskID, projectID)
			return func() tea.Msg {
				if err := m.store.EditNote(id, v[1]); err != nil {
					return actionMsg{err: err}
				}
				return actionMsg{status: "edited note " + id}
			}
		},
	}
	return nil
}

func (m *Model) openDeleteNoteForm() tea.Cmd {
	taskID, projectID, name, ok := m.noteTarget()
	if !ok {
		m.setStatus("select a project or task on the tasks page first")
		return nil
	}
	if len(m.ownNoteOptions(taskID, projectID)) == 0 {
		m.setStatus(name + " has no notes of its own to delete")
		return nil
	}
	options := func() []string { return m.ownNoteOptions(taskID, projectID) }
	m.mode = modeForm
	m.form = &form{
		Title:  "delete note of " + name,
		Fields: []field{{Label: "which", Default: options()[0], Options: options}},
		Validate: func(v []string) (int, string) {
			_, msg := m.pickNote(v[0], taskID, projectID)
			return 0, msg
		},
		Done: func(v []string) tea.Cmd {
			id, _ := m.pickNote(v[0], taskID, projectID)
			return func() tea.Msg {
				if err := m.store.DeleteNote(id); err != nil {
					return actionMsg{err: err}
				}
				return actionMsg{status: "deleted note " + id}
			}
		},
	}
	return nil
}

// noteLines is the notes section of the tasks page's detail: every note
// the selected row sees, own first, an inherited one marked with the owner
// it comes from.
func (m *Model) noteLines(r *trow) []string {
	taskID, projectID := "", ""
	if r.Project != nil {
		projectID = r.Project.ID
	} else {
		taskID = r.Task.ID
	}
	notes := m.store.NotesFor(taskID, projectID)
	lines := []string{cDim.Render(pad("notes", 9)) + fmt.Sprintf("%d", len(notes))}
	for _, n := range notes {
		from := "own"
		switch n.Relation {
		case store.NoteAncestor:
			from = "from " + n.Owner
		case store.NoteDescendant:
			from = "under " + n.Owner
		}
		text := strings.Split(n.Text, "\n")
		lines = append(lines, "  "+cDim.Render(n.ID+" "+time.Unix(n.Updated, 0).Format("01-02 15:04")+" · "+from))
		for _, l := range text {
			lines = append(lines, "    "+l)
		}
	}
	return lines
}
