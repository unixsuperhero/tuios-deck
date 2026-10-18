package web

import (
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/unixsuperhero/tuios-deck/internal/panes"
	"github.com/unixsuperhero/tuios-deck/internal/store"
)

// previewLines bounds the recent output a preview returns.
const previewLines = 6

func paneLine(p panes.Pane) string {
	kind := "terminal"
	if p.IsAgent {
		kind = "agent"
		if p.Harness != "" {
			kind = p.Harness
		}
	}
	s := strings.Join([]string{p.Name, p.Session, kind}, " · ")
	if p.State != "" {
		s += " · " + p.State
	}
	if p.NeedsYou {
		s += " · needs you"
	}
	return s
}

func paneRank(p panes.Pane) int {
	switch {
	case p.NeedsYou:
		return 0
	case p.IsAgent:
		return 1
	}
	return 2
}

// preview answers GET /api/preview with ?task=, ?project= or
// ?session=&window=: the head lines of metadata, and the last lines of the
// chosen live pane. The head always comes; lines is empty with an error
// when the capture failed or there is no pane to read.
func (s *Server) preview(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := callCtx(r)
	defer cancel()
	q := r.URL.Query()
	sessions, err := panes.List(ctx, s.Client)
	if err != nil {
		writeErr(w, err)
		return
	}
	var all []panes.Pane
	for _, se := range sessions {
		all = append(all, se.Panes...)
	}
	var head []string
	var chosen []panes.Pane
	switch {
	case q.Get("task") != "":
		t, err := s.findTask(q.Get("task"))
		if err != nil {
			badRequest(w, err.Error())
			return
		}
		head = []string{s.taskPath(*t), "status " + t.Status}
		chosen = s.relevantPanes(all, t, nil)
		if len(chosen) == 0 {
			head = append(head, "no live pane is assigned to this task")
		}
	case q.Get("project") != "":
		p := s.Store.ProjectByName(strings.TrimSpace(q.Get("project")))
		if p == nil {
			badRequest(w, "no project "+q.Get("project"))
			return
		}
		head = []string{p.Name, "status " + p.Status}
		chosen = s.relevantPanes(all, nil, p)
		if len(chosen) == 0 {
			head = append(head, "no live pane is assigned to this project")
		}
	case q.Get("window") != "":
		opt, session := strings.TrimSpace(q.Get("window")), q.Get("session")
		for _, p := range all {
			if (p.ID == opt || p.Name == opt) && (session == "" || p.Session == session) {
				chosen = []panes.Pane{p}
				break
			}
		}
		if len(chosen) == 0 {
			badRequest(w, "no pane "+opt)
			return
		}
		p := chosen[0]
		head = []string{paneLine(p)}
		if p.Cwd != "" {
			head = append(head, "cwd "+p.Cwd)
		}
		if p.Message != "" {
			head = append(head, p.Message)
		}
		if as := s.assignmentJSON(p.ID); as != nil {
			head = append(head, fmt.Sprintf("assigned to %v", s.assignedPath(p.ID)))
		}
	default:
		badRequest(w, "task, project or window is required")
		return
	}
	out := map[string]any{"head": head, "lines": []string{}}
	if len(chosen) > 0 {
		if q.Get("window") == "" {
			for i, p := range chosen {
				if i == 3 {
					out["head"] = append(out["head"].([]string), fmt.Sprintf("… and %d more", len(chosen)-3))
					break
				}
				mark := "  "
				if i == 0 {
					mark = "▸ "
				}
				out["head"] = append(out["head"].([]string), mark+paneLine(p))
			}
		}
		p := chosen[0]
		c, err := s.Client.CapturePane(ctx, p.Session, p.ID, previewLines*4, false)
		if err != nil {
			out["error"] = err.Error()
		} else {
			out["lines"] = recentLines(c.Content, previewLines)
		}
		out["session"], out["window"] = p.Session, p.ID
	}
	writeJSON(w, 200, out)
}

// assignedPath is "project › top task › task" for a window.
func (s *Server) assignedPath(windowID string) string {
	project, task, _ := s.Store.Resolve(windowID)
	var parts []string
	if project != nil {
		parts = append(parts, project.Name)
	}
	if task != nil {
		for _, c := range s.Store.Path(task.ID) {
			parts = append(parts, c.Title)
		}
	}
	return strings.Join(parts, " › ")
}

// relevantPanes are the live panes assigned to the task, or to anything
// under it, or to the project; the task's own come first, then needs-you
// ones, then agents.
func (s *Server) relevantPanes(all []panes.Pane, task *store.Task, project *store.Project) []panes.Pane {
	var direct, rest []panes.Pane
	for _, p := range all {
		if _, ok := s.Store.Assignment(p.ID); !ok {
			continue
		}
		pr, t, _ := s.Store.Resolve(p.ID)
		switch {
		case task != nil && t != nil && t.ID == task.ID:
			direct = append(direct, p)
		case task != nil && t != nil && pathHas(s.Store.Path(t.ID), task.ID):
			rest = append(rest, p)
		case project != nil && pr != nil && pr.ID == project.ID:
			rest = append(rest, p)
		}
	}
	for _, ps := range [][]panes.Pane{direct, rest} {
		sort.SliceStable(ps, func(i, j int) bool { return paneRank(ps[i]) < paneRank(ps[j]) })
	}
	return append(direct, rest...)
}

func pathHas(path []store.Task, id string) bool {
	for _, t := range path {
		if t.ID == id {
			return true
		}
	}
	return false
}

func recentLines(text string, n int) []string {
	lines := strings.Split(strings.TrimRight(text, " \t\r\n"), "\n")
	if len(lines) == 1 && lines[0] == "" {
		return []string{}
	}
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	for i, l := range lines {
		lines[i] = strings.TrimRight(l, " \t\r")
	}
	return lines
}
