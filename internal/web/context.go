package web

import (
	"fmt"
	"net/http"
	"strings"

	ctxitem "github.com/unixsuperhero/tuios-deck/internal/context"
	"github.com/unixsuperhero/tuios-deck/internal/panes"
	"github.com/unixsuperhero/tuios-deck/internal/store"
)

// contextView is an item with its project's name and its task's path, so
// the page draws a row without looking them up.
type contextView struct {
	store.ContextItem
	Project string `json:"project"`
	Task    string `json:"task"`
}

func (s *Server) contextJSON(c store.ContextItem) contextView {
	v := contextView{ContextItem: c}
	if p := s.Store.Project(c.ProjectID); p != nil {
		v.Project = p.Name
	}
	if t := s.Store.Task(c.TaskID); t != nil {
		v.Task = s.taskPath(*t)
	}
	return v
}

func truthy(v string) bool { return v == "1" || v == "true" || v == "yes" }

// listContext lists the items newest first. kind, project and task narrow
// it (a project or a task by id or by name), starred and pinned keep only
// those, window (with session) keeps one pane's items, and q keeps the items whose text holds it, ignoring case.
func (s *Server) listContext(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	projectID, taskID := "", ""
	if v := strings.TrimSpace(q.Get("project")); v != "" {
		projectID = v
		if p := s.Store.ProjectByName(v); p != nil {
			projectID = p.ID
		}
	}
	if v := strings.TrimSpace(q.Get("task")); v != "" {
		taskID = v
		if s.Store.Task(v) == nil {
			if t, err := s.findTask(v); err == nil {
				taskID = t.ID
			}
		}
	}
	text := strings.ToLower(q.Get("q"))
	out := []contextView{}
	for _, c := range s.Store.ContextItems() {
		switch {
		case q.Get("kind") != "" && c.Kind != q.Get("kind"),
			projectID != "" && c.ProjectID != projectID,
			taskID != "" && c.TaskID != taskID,
			q.Get("window") != "" && (c.Window != q.Get("window") || c.Session != q.Get("session")),
			truthy(q.Get("starred")) && !c.Starred,
			truthy(q.Get("pinned")) && !c.Pinned,
			text != "" && !strings.Contains(strings.ToLower(c.Text), text):
			continue
		}
		out = append(out, s.contextJSON(c))
	}
	writeJSON(w, 200, map[string]any{"items": out, "kinds": store.ContextKinds()})
}

// extractContext reads the pane's scrollback, activity, transcript and
// command history, and stores the items found there that are new.
func (s *Server) extractContext(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := callCtx(r)
	defer cancel()
	session, id := r.PathValue("session"), r.PathValue("id")
	src := ctxitem.Source{Session: session, Window: id, Pane: panes.ShortID(id)}
	if sessions, err := panes.List(ctx, s.Client); err == nil {
		for _, se := range sessions {
			for _, p := range se.Panes {
				if se.Name == session && (p.ID == id || p.Name == id) {
					src.Window, src.Pane, src.Cwd = p.ID, p.Name, p.Cwd
				}
			}
		}
	}
	s.mu.Lock()
	nonce := s.nonce
	s.mu.Unlock()
	items, counts, err := ctxitem.Gather(ctx, s.Client, src, nonce)
	if err != nil {
		writeErr(w, err)
		return
	}
	added, err := s.Store.AddContextItems(items)
	if err != nil {
		storeErr(w, err)
		return
	}
	kinds := map[string]int{}
	for _, it := range items {
		kinds[it.Kind]++
	}
	writeJSON(w, 200, map[string]any{
		"status": fmt.Sprintf("%d new context item(s) from %s, %d found", added, src.Pane, len(items)),
		"new":    added, "found": len(items), "kinds": kinds, "sources": counts,
	})
}

// patchContext stars, pins or assigns an item. A task or a project names
// the target the way an assignment does; both sent empty clear it.
func (s *Server) patchContext(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if s.Store.ContextItem(id) == nil {
		writeJSON(w, 404, map[string]any{"error": "no context item " + id})
		return
	}
	var in struct {
		Starred *bool `json:"starred"`
		Pinned  *bool `json:"pinned"`
		target
	}
	if err := readJSON(r, &in); err != nil {
		badRequest(w, "bad json: "+err.Error())
		return
	}
	if in.Starred != nil {
		if err := s.Store.SetContextStar(id, *in.Starred); err != nil {
			storeErr(w, err)
			return
		}
	}
	if in.Pinned != nil {
		if err := s.Store.SetContextPin(id, *in.Pinned); err != nil {
			storeErr(w, err)
			return
		}
	}
	if in.target.given() {
		taskID, projectID := "", ""
		if str(in.Task) != "" || str(in.TaskID) != "" || str(in.Project) != "" || str(in.ProjectID) != "" {
			var err error
			if taskID, projectID, err = s.resolveTarget(in.target); err != nil {
				badRequest(w, err.Error())
				return
			}
		}
		if err := s.Store.AssignContext(id, taskID, projectID); err != nil {
			storeErr(w, err)
			return
		}
	}
	writeJSON(w, 200, map[string]any{"status": "updated", "item": s.contextJSON(*s.Store.ContextItem(id))})
}

func (s *Server) deleteContext(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.Store.DeleteContextItem(id); err != nil {
		writeJSON(w, 404, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]any{"status": "deleted " + id})
}
