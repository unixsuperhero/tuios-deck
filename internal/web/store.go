package web

import (
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"strings"

	"github.com/unixsuperhero/tuios-deck/internal/store"
)

// themeIn is a theme as a request gives it; either half may be left out.
type themeIn struct {
	Color string `json:"color"`
	Glyph string `json:"glyph"`
}

// themeJSON is a theme as the API emits it: the colour name, the glyph and
// the colour as CSS hex.
func themeJSON(t store.Theme) map[string]any {
	return map[string]any{"color": t.Color, "glyph": t.Glyph, "css": t.CSS()}
}

// mergeTheme fills a request's theme over base: a colour or a glyph left
// out keeps base's. The colour must be one of the palette names.
func mergeTheme(base store.Theme, in themeIn) (store.Theme, error) {
	if in.Color != "" {
		known := false
		for _, c := range store.Palette() {
			if c == in.Color {
				known = true
			}
		}
		if !known {
			return base, fmt.Errorf("colour %q is not one of %s", in.Color, strings.Join(store.Palette(), " "))
		}
		base.Color = in.Color
	}
	if in.Glyph != "" {
		base.Glyph = in.Glyph
	}
	return base, nil
}

// newTheme is the theme for a new project or task: a random free colour
// from the store and the given glyph, with whatever the request set over it. Nil
// when the request set nothing, so the store picks.
func (s *Server) newTheme(in *themeIn, glyph string) (*store.Theme, error) {
	if in == nil {
		return nil, nil
	}
	base := s.Store.NextTheme()
	base.Glyph = glyph
	t, err := mergeTheme(base, *in)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// storeErr answers a store error: a failed write is a server error, anything
// else is the request's fault.
func storeErr(w http.ResponseWriter, err error) {
	var perr *fs.PathError
	var lerr *os.LinkError
	if errors.As(err, &perr) || errors.As(err, &lerr) {
		writeJSON(w, 500, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, 400, map[string]any{"error": err.Error()})
}

func badRequest(w http.ResponseWriter, msg string) {
	writeJSON(w, 400, map[string]any{"error": msg})
}

// taskPath is "project / top task / … / title", the same name the TUI
// completes and accepts.
func (s *Server) taskPath(t store.Task) string {
	parts := []string{"?"}
	if p := s.Store.Project(s.Store.ProjectOf(t.ID)); p != nil {
		parts[0] = p.Name
	}
	for _, c := range s.Store.Path(t.ID) {
		parts = append(parts, c.Title)
	}
	return strings.Join(parts, " / ")
}

// paneCounts is how many assigned panes each task has directly and each
// project has in all, by id.
func (s *Server) paneCounts() (byTask, byProject map[string]int) {
	byTask, byProject = map[string]int{}, map[string]int{}
	for _, a := range s.Store.Assignments() {
		projectID := a.ProjectID
		if a.TaskID != "" {
			byTask[a.TaskID]++
			projectID = s.Store.ProjectOf(a.TaskID)
		}
		if projectID != "" {
			byProject[projectID]++
		}
	}
	return byTask, byProject
}

func (s *Server) projectJSON(p store.Project, panes int) map[string]any {
	open := 0
	for _, t := range s.Store.Tasks() {
		if !store.IsFinal(t.Status) && s.Store.ProjectOf(t.ID) == p.ID {
			open++
		}
	}
	return map[string]any{
		"id": p.ID, "name": p.Name, "theme": themeJSON(p.Theme), "status": p.Status, "created": p.Created,
		"open_tasks": open, "panes": panes,
	}
}

func (s *Server) taskJSON(t store.Task, panes int) map[string]any {
	return map[string]any{
		"id": t.ID, "parent_id": t.ParentID, "project_id": s.Store.ProjectOf(t.ID), "own_project_id": t.ProjectID,
		"title": t.Title, "theme": themeJSON(t.Theme), "status": t.Status,
		"created": t.Created, "updated": t.Updated,
		"path": s.taskPath(t), "depth": len(s.Store.Path(t.ID)) - 1, "panes": panes,
	}
}

// projectsJSON is every project in the list shape.
func (s *Server) projectsJSON() []map[string]any {
	_, byProject := s.paneCounts()
	out := []map[string]any{}
	for _, p := range s.Store.Projects() {
		out = append(out, s.projectJSON(p, byProject[p.ID]))
	}
	return out
}

// tasksJSON is every task in the list shape, or those of one project.
func (s *Server) tasksJSON(projectID string) []map[string]any {
	byTask, _ := s.paneCounts()
	out := []map[string]any{}
	for _, t := range s.Store.Tasks() {
		if projectID != "" && s.Store.ProjectOf(t.ID) != projectID {
			continue
		}
		out = append(out, s.taskJSON(t, byTask[t.ID]))
	}
	return out
}

// assignmentJSON is what a window is assigned to, by name: nil when it has
// no assignment.
func (s *Server) assignmentJSON(windowID string) map[string]any {
	a, ok := s.Store.Assignment(windowID)
	if !ok {
		return nil
	}
	project, task, top := s.Store.Resolve(windowID)
	out := map[string]any{"task_id": a.TaskID, "project_id": a.ProjectID, "project": "", "task": "", "top": ""}
	if project != nil {
		out["project"] = project.Name
		out["project_id"] = project.ID
	}
	if task != nil {
		out["task"] = task.Title
	}
	if top != nil {
		out["top"] = top.Title
	}
	return out
}

// target names what to assign a pane to, by id or by name, the way the
// TUI's forms take it. Pointers tell a field left out from one sent empty.
type target struct {
	Task      *string `json:"task"`
	TaskID    *string `json:"task_id"`
	Project   *string `json:"project"`
	ProjectID *string `json:"project_id"`
}

func str(p *string) string {
	if p == nil {
		return ""
	}
	return strings.TrimSpace(*p)
}

// given says whether the request named any target field at all.
func (t target) given() bool {
	return t.Task != nil || t.TaskID != nil || t.Project != nil || t.ProjectID != nil
}

// findTask resolves a task field: a full path or a bare title, ignoring
// case. A bare title must name one task.
func (s *Server) findTask(text string) (*store.Task, error) {
	var byPath, byTitle []store.Task
	for _, t := range s.Store.Tasks() {
		if strings.EqualFold(s.taskPath(t), text) {
			byPath = append(byPath, t)
		}
		if strings.EqualFold(t.Title, text) {
			byTitle = append(byTitle, t)
		}
	}
	switch {
	case len(byPath) == 1:
		return &byPath[0], nil
	case len(byTitle) == 1:
		return &byTitle[0], nil
	case len(byTitle) > 1:
		return nil, fmt.Errorf("task %s is ambiguous; give its path", text)
	}
	return nil, fmt.Errorf("no task %s", text)
}

// resolveTarget turns a target into the ids to assign, with the TUI's rule:
// a task implies its project, so with a task the project is ignored;
// without one a project is required.
func (s *Server) resolveTarget(t target) (taskID, projectID string, err error) {
	switch {
	case str(t.TaskID) != "":
		if s.Store.Task(str(t.TaskID)) == nil {
			return "", "", fmt.Errorf("no task %s", str(t.TaskID))
		}
		return str(t.TaskID), "", nil
	case str(t.Task) != "":
		task, err := s.findTask(str(t.Task))
		if err != nil {
			return "", "", err
		}
		return task.ID, "", nil
	case str(t.ProjectID) != "":
		if s.Store.Project(str(t.ProjectID)) == nil {
			return "", "", fmt.Errorf("no project %s", str(t.ProjectID))
		}
		return "", str(t.ProjectID), nil
	case str(t.Project) != "":
		p := s.Store.ProjectByName(str(t.Project))
		if p == nil {
			return "", "", fmt.Errorf("no project %s", str(t.Project))
		}
		return "", p.ID, nil
	}
	return "", "", errors.New("a task or a project is required")
}

// palette is the 16 colour names a theme may take, with their CSS hex, in
// palette order.
func (s *Server) palette(w http.ResponseWriter, r *http.Request) {
	colors := []map[string]string{}
	for _, name := range store.Palette() {
		colors = append(colors, map[string]string{"name": name, "css": store.Theme{Color: name}.CSS()})
	}
	writeJSON(w, 200, map[string]any{"colors": colors})
}

// projects

func (s *Server) listProjects(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{"projects": s.projectsJSON()})
}

func (s *Server) createProject(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name  string   `json:"name"`
		Theme *themeIn `json:"theme"`
	}
	if err := readJSON(r, &in); err != nil {
		badRequest(w, "bad json: "+err.Error())
		return
	}
	theme, err := s.newTheme(in.Theme, "◆")
	if err != nil {
		badRequest(w, err.Error())
		return
	}
	p, err := s.Store.AddProject(in.Name, theme)
	if err != nil {
		storeErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"status": "created project " + p.Name, "project": s.projectJSON(p, 0)})
}

func (s *Server) patchProject(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	p := s.Store.Project(id)
	if p == nil {
		writeJSON(w, 404, map[string]any{"error": "no project " + id})
		return
	}
	var in struct {
		Name   *string  `json:"name"`
		Status *string  `json:"status"`
		Theme  *themeIn `json:"theme"`
	}
	if err := readJSON(r, &in); err != nil {
		badRequest(w, "bad json: "+err.Error())
		return
	}
	if in.Name != nil {
		if err := s.Store.RenameProject(id, *in.Name); err != nil {
			storeErr(w, err)
			return
		}
	}
	if in.Status != nil {
		if err := s.Store.SetProjectStatus(id, *in.Status); err != nil {
			storeErr(w, err)
			return
		}
	}
	if in.Theme != nil {
		t, err := mergeTheme(p.Theme, *in.Theme)
		if err != nil {
			badRequest(w, err.Error())
			return
		}
		if err := s.Store.SetProjectTheme(id, t); err != nil {
			storeErr(w, err)
			return
		}
	}
	_, byProject := s.paneCounts()
	writeJSON(w, 200, map[string]any{"status": "updated", "project": s.projectJSON(*s.Store.Project(id), byProject[id])})
}

func (s *Server) deleteProject(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	p := s.Store.Project(id)
	if p == nil {
		writeJSON(w, 404, map[string]any{"error": "no project " + id})
		return
	}
	tasks, assignments := 0, 0
	for _, t := range s.Store.Tasks() {
		if s.Store.ProjectOf(t.ID) == id {
			tasks++
		}
	}
	for _, a := range s.Store.Assignments() {
		if a.ProjectID == id || (a.TaskID != "" && s.Store.ProjectOf(a.TaskID) == id) {
			assignments++
		}
	}
	if err := s.Store.DeleteProject(id); err != nil {
		storeErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{
		"status":  fmt.Sprintf("deleted project %s with %d task(s) and %d assignment(s)", p.Name, tasks, assignments),
		"deleted": map[string]any{"project": p.Name, "project_id": id, "tasks": tasks, "assignments": assignments},
	})
}

// tasks

func (s *Server) listTasks(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{"tasks": s.tasksJSON(r.URL.Query().Get("project"))})
}

func (s *Server) createTask(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ProjectID string   `json:"project_id"`
		ParentID  string   `json:"parent_id"`
		Title     string   `json:"title"`
		Theme     *themeIn `json:"theme"`
	}
	if err := readJSON(r, &in); err != nil {
		badRequest(w, "bad json: "+err.Error())
		return
	}
	theme, err := s.newTheme(in.Theme, "●")
	if err != nil {
		badRequest(w, err.Error())
		return
	}
	t, err := s.Store.AddTask(in.ProjectID, in.ParentID, in.Title, theme)
	if err != nil {
		storeErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"status": "created task " + s.taskPath(t), "task": s.taskJSON(t, 0)})
}

func (s *Server) patchTask(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	t := s.Store.Task(id)
	if t == nil {
		writeJSON(w, 404, map[string]any{"error": "no task " + id})
		return
	}
	var in struct {
		Title  *string  `json:"title"`
		Status *string  `json:"status"`
		Theme  *themeIn `json:"theme"`
	}
	if err := readJSON(r, &in); err != nil {
		badRequest(w, "bad json: "+err.Error())
		return
	}
	if in.Title != nil {
		if err := s.Store.RenameTask(id, *in.Title); err != nil {
			storeErr(w, err)
			return
		}
	}
	if in.Status != nil {
		if err := s.Store.SetTaskStatus(id, *in.Status); err != nil {
			storeErr(w, err)
			return
		}
	}
	if in.Theme != nil {
		th, err := mergeTheme(t.Theme, *in.Theme)
		if err != nil {
			badRequest(w, err.Error())
			return
		}
		if err := s.Store.SetTaskTheme(id, th); err != nil {
			storeErr(w, err)
			return
		}
	}
	byTask, _ := s.paneCounts()
	writeJSON(w, 200, map[string]any{"status": "updated", "task": s.taskJSON(*s.Store.Task(id), byTask[id])})
}

func (s *Server) deleteTask(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	t := s.Store.Task(id)
	if t == nil {
		writeJSON(w, 404, map[string]any{"error": "no task " + id})
		return
	}
	gone := map[string]bool{}
	s.collect(id, gone)
	assignments := 0
	for _, a := range s.Store.Assignments() {
		if gone[a.TaskID] {
			assignments++
		}
	}
	path := s.taskPath(*t)
	if err := s.Store.DeleteTask(id); err != nil {
		storeErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{
		"status":  fmt.Sprintf("deleted task %s with %d subtask(s) and %d assignment(s)", path, len(gone)-1, assignments),
		"deleted": map[string]any{"task": path, "task_id": id, "subtasks": len(gone) - 1, "assignments": assignments},
	})
}

// collect marks id and every descendant in gone.
func (s *Server) collect(id string, gone map[string]bool) {
	if gone[id] {
		return
	}
	gone[id] = true
	for _, c := range s.Store.Children(id) {
		s.collect(c.ID, gone)
	}
}

// assignments

func (s *Server) listAssignments(w http.ResponseWriter, r *http.Request) {
	out := map[string]any{}
	for windowID, a := range s.Store.Assignments() {
		project, task, top := s.Store.Resolve(windowID)
		resolved := map[string]any{"project": "", "task": "", "top": ""}
		if project != nil {
			resolved["project"] = project.Name
		}
		if task != nil {
			resolved["task"] = task.Title
		}
		if top != nil {
			resolved["top"] = top.Title
		}
		out[windowID] = map[string]any{"session": a.Session, "task_id": a.TaskID, "project_id": a.ProjectID, "resolved": resolved}
	}
	writeJSON(w, 200, map[string]any{"assignments": out})
}

func (s *Server) putAssignment(w http.ResponseWriter, r *http.Request) {
	var in target
	if err := readJSON(r, &in); err != nil {
		badRequest(w, "bad json: "+err.Error())
		return
	}
	taskID, projectID, err := s.resolveTarget(in)
	if err != nil {
		badRequest(w, err.Error())
		return
	}
	session, id := r.PathValue("session"), r.PathValue("id")
	if err := s.Store.Assign(id, session, taskID, projectID); err != nil {
		storeErr(w, err)
		return
	}
	a := s.assignmentJSON(id)
	writeJSON(w, 200, map[string]any{"status": "assigned to " + assignmentText(a), "assignment": a})
}

func (s *Server) deleteAssignment(w http.ResponseWriter, r *http.Request) {
	if err := s.Store.Unassign(r.PathValue("id")); err != nil {
		storeErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"status": "unassigned"})
}

// assignmentText is "project › top › task" for a status line.
func assignmentText(a map[string]any) string {
	if a == nil {
		return "nothing"
	}
	parts := []string{fmt.Sprint(a["project"])}
	if top := fmt.Sprint(a["top"]); top != "" {
		parts = append(parts, top)
	}
	if task := fmt.Sprint(a["task"]); task != "" && task != fmt.Sprint(a["top"]) {
		parts = append(parts, task)
	}
	return strings.Join(parts, " › ")
}

// assignNew ties a window the daemon just made to the target the request
// gave and extends the status with where it went, or why it could not.
func (s *Server) assignNew(windowID, session, taskID, projectID, status string) string {
	if taskID == "" && projectID == "" {
		return status
	}
	if err := s.Store.Assign(windowID, session, taskID, projectID); err != nil {
		return status + "; not assigned: " + err.Error()
	}
	return status + " · " + assignmentText(s.assignmentJSON(windowID))
}
