package web

import (
	"net/http"
	"strings"

	"github.com/unixsuperhero/tuios-deck/internal/store"
)

func noteJSON(n store.VisibleNote) map[string]any {
	return map[string]any{
		"id": n.ID, "text": n.Text, "relation": n.Relation, "owner": n.Owner,
		"task_id": n.TaskID, "project_id": n.ProjectID,
		"created": n.Created, "updated": n.Updated,
	}
}

// listNotes answers the notes a task (?task=) or a project (?project=)
// sees: its own, its ancestors' and its descendants', each marked with its
// relation.
func (s *Server) listNotes(w http.ResponseWriter, r *http.Request) {
	taskID, projectID := r.URL.Query().Get("task"), r.URL.Query().Get("project")
	if (taskID == "") == (projectID == "") {
		badRequest(w, "give task or project")
		return
	}
	if (taskID != "" && s.Store.Task(taskID) == nil) || (projectID != "" && s.Store.Project(projectID) == nil) {
		writeJSON(w, 404, map[string]any{"error": "no such task or project"})
		return
	}
	out := []map[string]any{}
	for _, n := range s.Store.NotesFor(taskID, projectID) {
		out = append(out, noteJSON(n))
	}
	writeJSON(w, 200, map[string]any{"notes": out})
}

func (s *Server) createNote(w http.ResponseWriter, r *http.Request) {
	var in struct {
		TaskID    string `json:"task_id"`
		ProjectID string `json:"project_id"`
		Text      string `json:"text"`
	}
	if err := readJSON(r, &in); err != nil {
		badRequest(w, "bad json: "+err.Error())
		return
	}
	n, err := s.Store.AddNote(in.TaskID, in.ProjectID, in.Text)
	if err != nil {
		storeErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"status": "added note " + n.ID, "note": n})
}

func (s *Server) patchNote(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if s.Store.Note(id) == nil {
		writeJSON(w, 404, map[string]any{"error": "no note " + id})
		return
	}
	var in struct {
		Text *string `json:"text"`
	}
	if err := readJSON(r, &in); err != nil || in.Text == nil {
		badRequest(w, "give text")
		return
	}
	if err := s.Store.EditNote(id, *in.Text); err != nil {
		storeErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"status": "edited note " + id, "note": s.Store.Note(id)})
}

func (s *Server) deleteNote(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if s.Store.Note(strings.TrimSpace(id)) == nil {
		writeJSON(w, 404, map[string]any{"error": "no note " + id})
		return
	}
	if err := s.Store.DeleteNote(id); err != nil {
		storeErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"status": "deleted note " + id})
}
