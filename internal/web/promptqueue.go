package web

import (
	"context"
	"net/http"
	"strings"

	"github.com/unixsuperhero/tuios-deck/internal/panes"
	"github.com/unixsuperhero/tuios-deck/internal/prompts"
	"github.com/unixsuperhero/tuios-deck/internal/store"
)

// queuedView is a queued prompt with its project's name and its task's
// path, so the page draws a row without looking them up.
type queuedView struct {
	store.QueuedPrompt
	Project string `json:"project"`
	Task    string `json:"task"`
}

func (s *Server) queuedJSON(q store.QueuedPrompt) queuedView {
	v := queuedView{QueuedPrompt: q}
	if q.TaskID != "" {
		if t := s.Store.Task(q.TaskID); t != nil {
			v.Task = s.taskPath(*t)
		}
		q.ProjectID = s.Store.ProjectOf(q.TaskID)
	}
	if p := s.Store.Project(q.ProjectID); p != nil {
		v.Project = p.Name
	}
	return v
}

func (s *Server) listQueued(w http.ResponseWriter, r *http.Request) {
	out := []queuedView{}
	for _, q := range s.Store.Queued() {
		out = append(out, s.queuedJSON(q))
	}
	writeJSON(w, 200, map[string]any{"queued": out})
}

type queuedIn struct {
	target
	Text    string  `json:"text"`
	Preset  string  `json:"preset"`
	Agent   *string `json:"agent"`
	Session *string `json:"session"`
	Cwd     *string `json:"cwd"`
	Window  *string `json:"window"`
}

// queuedFrom is the prompt a request describes, over base. The preset's
// prefix goes in front of the text, and its cwd stands in for an empty one,
// now: the stored text is final. A window means the prompt is for that
// pane, whose session comes from the listing.
func (s *Server) queuedFrom(r *http.Request, in queuedIn, base store.QueuedPrompt) (store.QueuedPrompt, error) {
	q := base
	if in.target.given() {
		taskID, projectID, err := s.resolveTarget(in.target)
		if err != nil {
			return q, err
		}
		q.TaskID, q.ProjectID = taskID, projectID
	}
	if in.Agent != nil {
		q.Agent = strings.TrimSpace(*in.Agent)
	}
	if in.Session != nil {
		q.Session = strings.TrimSpace(*in.Session)
	}
	if in.Cwd != nil {
		q.Cwd = strings.TrimSpace(*in.Cwd)
	}
	if in.Window != nil {
		q.Window = strings.TrimSpace(*in.Window)
	}
	if in.Text != "" {
		q.Text = in.Text
	}
	cwd, text, err := s.applyPreset(in.Preset, q.Cwd, in.Text)
	if err != nil {
		return q, err
	}
	q.Cwd = cwd
	if in.Text != "" {
		q.Text = text
	}
	if in.Preset != "" {
		q.Preset = in.Preset
	}
	if q.Window != "" {
		ctx, cancel := callCtx(r)
		defer cancel()
		list, err := panes.List(ctx, s.Client)
		if err != nil {
			return q, err
		}
		found := false
		for _, se := range list {
			for _, p := range se.Panes {
				if p.ID == q.Window {
					q.Session, found = p.Session, true
				}
			}
		}
		if !found {
			return q, errNoPane
		}
	}
	return q, nil
}

type paneError string

func (e paneError) Error() string { return string(e) }

const errNoPane = paneError("no such pane; clear the pane to start an agent instead")

func (s *Server) createQueued(w http.ResponseWriter, r *http.Request) {
	var in queuedIn
	if err := readJSON(r, &in); err != nil {
		badRequest(w, "bad json")
		return
	}
	base := store.QueuedPrompt{Agent: "claude"}
	q, err := s.queuedFrom(r, in, base)
	if err != nil {
		badRequest(w, err.Error())
		return
	}
	saved, err := s.Store.QueuePrompt(q)
	if err != nil {
		badRequest(w, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"status": "queued " + saved.ID, "queued": s.queuedJSON(saved)})
}

func (s *Server) patchQueued(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	cur := s.Store.QueuedPrompt(id)
	if cur == nil {
		writeJSON(w, 404, map[string]any{"error": "no queued prompt " + id})
		return
	}
	var in queuedIn
	if err := readJSON(r, &in); err != nil {
		badRequest(w, "bad json")
		return
	}
	in.Preset = "" // the stored text already has its prefix
	q, err := s.queuedFrom(r, in, *cur)
	if err != nil {
		badRequest(w, err.Error())
		return
	}
	if err := s.Store.UpdateQueued(q); err != nil {
		badRequest(w, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"status": "saved the queued prompt", "queued": s.queuedJSON(*s.Store.QueuedPrompt(id))})
}

func (s *Server) deleteQueued(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.Store.DequeuePrompt(id); err != nil {
		writeJSON(w, 404, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]any{"status": "deleted the queued prompt", "id": id})
}

// startQueued sends the prompt. It leaves the queue only when it went; a
// failure answers 502 with the reason and the prompt stays.
func (s *Server) startQueued(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	q := s.Store.QueuedPrompt(id)
	if q == nil {
		writeJSON(w, 404, map[string]any{"error": "no queued prompt " + id})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), prompts.StartTimeout)
	defer cancel()
	status, err := prompts.Dispatch(ctx, s.Client, s.Store, *q)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error(), "kept": s.Store.QueuedPrompt(id) != nil})
		return
	}
	writeJSON(w, 200, map[string]any{"status": status, "id": id})
}
