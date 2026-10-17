// Package store holds the deck's projects, tasks and pane assignments in one
// JSON file under the user's data folder. Tasks and subtasks share one
// self-referencing table: a task with an empty parent_id is top-level and
// carries the project_id; a subtask's project is its root's.
package store

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Project groups tasks. Status is "open", "done" or "skipped", like a
// task's.
type Project struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Theme   Theme  `json:"theme"`
	Status  string `json:"status"`
	Created int64  `json:"created"`
}

// Task is a task or a subtask. ParentID is "" for a top-level task, and
// ProjectID is set only then.
type Task struct {
	ID        string `json:"id"`
	ParentID  string `json:"parent_id"`
	ProjectID string `json:"project_id"`
	Title     string `json:"title"`
	Theme     Theme  `json:"theme"`
	Status    string `json:"status"`
	Created   int64  `json:"created"`
	Updated   int64  `json:"updated"`
}

// Assignment ties a pane, keyed by window id, to a task or a project.
// TaskID wins; ProjectID alone is allowed.
type Assignment struct {
	Session   string `json:"session"`
	TaskID    string `json:"task_id"`
	ProjectID string `json:"project_id"`
}

// Store is the file's contents plus where it lives.
type Store struct {
	path        string
	mu          sync.Mutex
	projects    []Project
	tasks       []Task
	assignments map[string]Assignment
}

// file is the JSON shape on disk.
type file struct {
	Version     int                   `json:"version"`
	Projects    []Project             `json:"projects"`
	Tasks       []Task                `json:"tasks"`
	Assignments map[string]Assignment `json:"assignments"`
}

const version = 1

// DataDir is the deck's data folder: $XDG_DATA_HOME/tuios-deck, else
// ~/.local/share/tuios-deck.
func DataDir() string {
	base := os.Getenv("XDG_DATA_HOME")
	if base == "" {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, ".local", "share")
	}
	return filepath.Join(base, "tuios-deck")
}

// DefaultPath is the store file: $TUIOS_DECK_DATA, else <DataDir>/deck.json.
func DefaultPath() string {
	if p := os.Getenv("TUIOS_DECK_DATA"); p != "" {
		return p
	}
	return filepath.Join(DataDir(), "deck.json")
}

// Open reads the store at path. A missing file is an empty store.
func Open(path string) (*Store, error) {
	s := &Store{path: path, assignments: map[string]Assignment{}}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	var f file
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if f.Version > version {
		return nil, fmt.Errorf("%s: version %d is newer than this deck knows", path, f.Version)
	}
	s.projects = f.Projects
	s.tasks = f.Tasks
	// Files written before projects had a status read as open.
	for i := range s.projects {
		if s.projects[i].Status == "" {
			s.projects[i].Status = "open"
		}
	}
	for i := range s.tasks {
		if s.tasks[i].Status == "" {
			s.tasks[i].Status = "open"
		}
	}
	if f.Assignments != nil {
		s.assignments = f.Assignments
	}
	return s, nil
}

// File is the path the store reads and writes.
func (s *Store) File() string { return s.path }

// Save writes the store to its file through a temp file and a rename, so a
// reader never sees a partial file.
func (s *Store) Save() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.save()
}

func (s *Store) save() error {
	f := file{Version: version, Projects: s.projects, Tasks: s.tasks, Assignments: s.assignments}
	if f.Projects == nil {
		f.Projects = []Project{}
	}
	if f.Tasks == nil {
		f.Tasks = []Task{}
	}
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".deck-*.json")
	if err != nil {
		return err
	}
	name := tmp.Name()
	_, werr := tmp.Write(append(data, '\n'))
	if cerr := tmp.Chmod(0o600); werr == nil {
		werr = cerr
	}
	if cerr := tmp.Close(); werr == nil {
		werr = cerr
	}
	if werr != nil {
		os.Remove(name)
		return werr
	}
	if err := os.Rename(name, s.path); err != nil {
		os.Remove(name)
		return err
	}
	return nil
}

func (s *Store) newID(prefix string) string {
	for {
		b := make([]byte, 3)
		rand.Read(b)
		id := prefix + hex.EncodeToString(b)
		if s.projectIndex(id) < 0 && s.taskIndex(id) < 0 {
			return id
		}
	}
}

func now() int64 { return time.Now().Unix() }

func (s *Store) projectIndex(id string) int {
	for i, p := range s.projects {
		if p.ID == id {
			return i
		}
	}
	return -1
}

func (s *Store) taskIndex(id string) int {
	for i, t := range s.tasks {
		if t.ID == id {
			return i
		}
	}
	return -1
}

// AddProject creates a project. The name must be non-empty and unique,
// ignoring case. With no theme it takes a random free colour and the
// project glyph.
func (s *Store) AddProject(name string, theme *Theme) (Project, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	name = strings.TrimSpace(name)
	if name == "" {
		return Project{}, errors.New("project name is empty")
	}
	if s.projectByName(name) >= 0 {
		return Project{}, fmt.Errorf("project %q exists", name)
	}
	p := Project{ID: s.newID("p_"), Name: name, Status: "open", Created: now()}
	if theme != nil {
		p.Theme = *theme
	} else {
		p.Theme = s.nextTheme()
		p.Theme.Glyph = "◆"
	}
	s.projects = append(s.projects, p)
	return p, s.save()
}

// RenameProject changes a project's name, which stays unique.
func (s *Store) RenameProject(id, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	i := s.projectIndex(id)
	if i < 0 {
		return fmt.Errorf("no project %s", id)
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return errors.New("project name is empty")
	}
	if j := s.projectByName(name); j >= 0 && j != i {
		return fmt.Errorf("project %q exists", name)
	}
	s.projects[i].Name = name
	return s.save()
}

// SetProjectTheme changes a project's theme.
func (s *Store) SetProjectTheme(id string, theme Theme) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	i := s.projectIndex(id)
	if i < 0 {
		return fmt.Errorf("no project %s", id)
	}
	s.projects[i].Theme = theme
	return s.save()
}

// SetProjectStatus marks a project "open", "done" or "skipped".
func (s *Store) SetProjectStatus(id, status string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	i := s.projectIndex(id)
	if i < 0 {
		return fmt.Errorf("no project %s", id)
	}
	if err := checkStatus(status); err != nil {
		return err
	}
	s.projects[i].Status = status
	return s.save()
}

// DeleteProject removes a project, its tasks and subtasks, and every
// assignment that pointed at any of them.
func (s *Store) DeleteProject(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	i := s.projectIndex(id)
	if i < 0 {
		return fmt.Errorf("no project %s", id)
	}
	gone := map[string]bool{}
	for _, t := range s.tasks {
		if t.ParentID == "" && t.ProjectID == id {
			s.collect(t.ID, gone)
		}
	}
	s.removeTasks(gone)
	for w, a := range s.assignments {
		if a.ProjectID == id {
			delete(s.assignments, w)
		}
	}
	s.projects = append(s.projects[:i], s.projects[i+1:]...)
	return s.save()
}

// AddTask creates a task. With a parentID it is a subtask and projectID is
// ignored; without one projectID names the project it belongs to. With no
// theme it takes a random free colour.
func (s *Store) AddTask(projectID, parentID, title string, theme *Theme) (Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	title = strings.TrimSpace(title)
	if title == "" {
		return Task{}, errors.New("task title is empty")
	}
	if parentID != "" {
		if s.taskIndex(parentID) < 0 {
			return Task{}, fmt.Errorf("no task %s", parentID)
		}
		projectID = ""
	} else {
		if projectID == "" {
			return Task{}, errors.New("a top-level task needs a project")
		}
		if s.projectIndex(projectID) < 0 {
			return Task{}, fmt.Errorf("no project %s", projectID)
		}
	}
	ts := now()
	t := Task{ID: s.newID("t_"), ParentID: parentID, ProjectID: projectID, Title: title, Status: "open", Created: ts, Updated: ts}
	if theme != nil {
		t.Theme = *theme
	} else {
		t.Theme = s.nextTheme()
	}
	s.tasks = append(s.tasks, t)
	return t, s.save()
}

// RenameTask changes a task's title.
func (s *Store) RenameTask(id, title string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	i := s.taskIndex(id)
	if i < 0 {
		return fmt.Errorf("no task %s", id)
	}
	title = strings.TrimSpace(title)
	if title == "" {
		return errors.New("task title is empty")
	}
	s.tasks[i].Title = title
	s.tasks[i].Updated = now()
	return s.save()
}

// SetTaskTheme changes a task's theme.
func (s *Store) SetTaskTheme(id string, theme Theme) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	i := s.taskIndex(id)
	if i < 0 {
		return fmt.Errorf("no task %s", id)
	}
	s.tasks[i].Theme = theme
	s.tasks[i].Updated = now()
	return s.save()
}

// SetTaskStatus marks a task "open", "done" or "skipped".
func (s *Store) SetTaskStatus(id, status string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	i := s.taskIndex(id)
	if i < 0 {
		return fmt.Errorf("no task %s", id)
	}
	if err := checkStatus(status); err != nil {
		return err
	}
	s.tasks[i].Status = status
	s.tasks[i].Updated = now()
	return s.save()
}

// Statuses is every status a project or task may have, in the order the
// TUI cycles through them.
var Statuses = []string{"open", "done", "skipped"}

// IsFinal says whether a status is finished with: done or skipped. A final
// item no longer counts as open and no longer holds its theme colour.
func IsFinal(status string) bool { return status == "done" || status == "skipped" }

func checkStatus(status string) error {
	for _, st := range Statuses {
		if st == status {
			return nil
		}
	}
	return fmt.Errorf("status %q is not open, done or skipped", status)
}

// DeleteTask removes a task, its subtasks, and the assignments that pointed
// at any of them.
func (s *Store) DeleteTask(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.taskIndex(id) < 0 {
		return fmt.Errorf("no task %s", id)
	}
	gone := map[string]bool{}
	s.collect(id, gone)
	s.removeTasks(gone)
	return s.save()
}

// collect marks id and every descendant in gone.
func (s *Store) collect(id string, gone map[string]bool) {
	if gone[id] {
		return
	}
	gone[id] = true
	for _, t := range s.tasks {
		if t.ParentID == id {
			s.collect(t.ID, gone)
		}
	}
}

func (s *Store) removeTasks(gone map[string]bool) {
	kept := s.tasks[:0]
	for _, t := range s.tasks {
		if !gone[t.ID] {
			kept = append(kept, t)
		}
	}
	s.tasks = kept
	for w, a := range s.assignments {
		if gone[a.TaskID] {
			delete(s.assignments, w)
		}
	}
}

// Assign ties a window to a task or, with no task, to a project. Exactly
// one of taskID and projectID is given and it must exist.
func (s *Store) Assign(windowID, session, taskID, projectID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if windowID == "" {
		return errors.New("window id is empty")
	}
	switch {
	case taskID != "" && projectID != "":
		return errors.New("assign to a task or a project, not both")
	case taskID != "":
		if s.taskIndex(taskID) < 0 {
			return fmt.Errorf("no task %s", taskID)
		}
	case projectID != "":
		if s.projectIndex(projectID) < 0 {
			return fmt.Errorf("no project %s", projectID)
		}
	default:
		return errors.New("assign needs a task or a project")
	}
	s.assignments[windowID] = Assignment{Session: session, TaskID: taskID, ProjectID: projectID}
	return s.save()
}

// Unassign drops a window's assignment. A window with none is fine.
func (s *Store) Unassign(windowID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.assignments[windowID]; !ok {
		return nil
	}
	delete(s.assignments, windowID)
	return s.save()
}

// Prune drops assignments of windows that are no longer live.
func (s *Store) Prune(live map[string]bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	changed := false
	for w := range s.assignments {
		if !live[w] {
			delete(s.assignments, w)
			changed = true
		}
	}
	if !changed {
		return nil
	}
	return s.save()
}

// Projects is every project sorted by name.
func (s *Store) Projects() []Project {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Project, len(s.projects))
	copy(out, s.projects)
	sort.SliceStable(out, func(i, j int) bool {
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out
}

// Project is the project with that id, or nil.
func (s *Store) Project(id string) *Project {
	s.mu.Lock()
	defer s.mu.Unlock()
	if i := s.projectIndex(id); i >= 0 {
		p := s.projects[i]
		return &p
	}
	return nil
}

// ProjectByName is the project with that name, ignoring case, or nil.
func (s *Store) ProjectByName(name string) *Project {
	s.mu.Lock()
	defer s.mu.Unlock()
	if i := s.projectByName(strings.TrimSpace(name)); i >= 0 {
		p := s.projects[i]
		return &p
	}
	return nil
}

func (s *Store) projectByName(name string) int {
	for i, p := range s.projects {
		if strings.EqualFold(p.Name, name) {
			return i
		}
	}
	return -1
}

// Tasks is every task and subtask in creation order.
func (s *Store) Tasks() []Task {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Task, len(s.tasks))
	copy(out, s.tasks)
	return out
}

// Task is the task with that id, or nil.
func (s *Store) Task(id string) *Task {
	s.mu.Lock()
	defer s.mu.Unlock()
	if i := s.taskIndex(id); i >= 0 {
		t := s.tasks[i]
		return &t
	}
	return nil
}

// TaskByTitle is the task in a project, at any depth, with that title
// ignoring case, or nil.
func (s *Store) TaskByTitle(projectID, title string) *Task {
	s.mu.Lock()
	defer s.mu.Unlock()
	title = strings.TrimSpace(title)
	for _, t := range s.tasks {
		if strings.EqualFold(t.Title, title) && s.projectOf(t.ID) == projectID {
			return &t
		}
	}
	return nil
}

// Roots is a project's top-level tasks, oldest first.
func (s *Store) Roots(projectID string) []Task {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Task
	for _, t := range s.tasks {
		if t.ParentID == "" && t.ProjectID == projectID {
			out = append(out, t)
		}
	}
	sortTasks(out)
	return out
}

// Children is a task's direct subtasks, oldest first.
func (s *Store) Children(parentID string) []Task {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Task
	for _, t := range s.tasks {
		if parentID != "" && t.ParentID == parentID {
			out = append(out, t)
		}
	}
	sortTasks(out)
	return out
}

func sortTasks(ts []Task) {
	sort.SliceStable(ts, func(i, j int) bool {
		if ts[i].Created != ts[j].Created {
			return ts[i].Created < ts[j].Created
		}
		return ts[i].ID < ts[j].ID
	})
}

// Assignments is a copy of every assignment keyed by window id.
func (s *Store) Assignments() map[string]Assignment {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]Assignment, len(s.assignments))
	for w, a := range s.assignments {
		out[w] = a
	}
	return out
}

// Assignment is the window's assignment, if it has one.
func (s *Store) Assignment(windowID string) (Assignment, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, ok := s.assignments[windowID]
	return a, ok
}

// Path is the chain from the root task down to the task, or nil when the
// task does not exist.
func (s *Store) Path(taskID string) []Task {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.chain(taskID)
}

func (s *Store) chain(taskID string) []Task {
	var chain []Task
	id := taskID
	for id != "" && len(chain) <= len(s.tasks) {
		i := s.taskIndex(id)
		if i < 0 {
			break
		}
		chain = append(chain, s.tasks[i])
		id = s.tasks[i].ParentID
	}
	for i, j := 0, len(chain)-1; i < j; i, j = i+1, j-1 {
		chain[i], chain[j] = chain[j], chain[i]
	}
	return chain
}

// TopOf is the root ancestor of a task (the task itself when top-level), or
// nil when the task does not exist.
func (s *Store) TopOf(taskID string) *Task {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.topOf(taskID)
}

func (s *Store) topOf(taskID string) *Task {
	chain := s.chain(taskID)
	if len(chain) == 0 {
		return nil
	}
	top := chain[0]
	return &top
}

// ProjectOf is the project id a task belongs to, found at its root, or ""
// when the task does not exist.
func (s *Store) ProjectOf(taskID string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.projectOf(taskID)
}

func (s *Store) projectOf(taskID string) string {
	if top := s.topOf(taskID); top != nil {
		return top.ProjectID
	}
	return ""
}

// Resolve is what a window is assigned to: its project, its task when it
// has one, and that task's root. Each is nil when there is none.
func (s *Store) Resolve(windowID string) (project *Project, task *Task, top *Task) {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, ok := s.assignments[windowID]
	if !ok {
		return nil, nil, nil
	}
	projectID := a.ProjectID
	if a.TaskID != "" {
		if i := s.taskIndex(a.TaskID); i >= 0 {
			t := s.tasks[i]
			task = &t
			top = s.topOf(a.TaskID)
			projectID = top.ProjectID
		}
	}
	if i := s.projectIndex(projectID); i >= 0 {
		p := s.projects[i]
		project = &p
	}
	return project, task, top
}
