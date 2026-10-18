// Package web serves the deck in a browser: a JSON API over the daemon, an
// event stream, a WebSocket bridge to stream-pane for xterm.js, and the
// embedded page.
package web

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/unixsuperhero/tuios-deck/internal/capture"
	"github.com/unixsuperhero/tuios-deck/internal/daemon"
	"github.com/unixsuperhero/tuios-deck/internal/panes"
	"github.com/unixsuperhero/tuios-deck/internal/store"
)

//go:embed static
var static embed.FS

const callTimeout = 10 * time.Second

// Server holds the daemon client, the store and the presence.
type Server struct {
	Client     *daemon.Client
	Store      *store.Store
	Version    string
	CaptureDir string

	mu        sync.Mutex
	nonce     string
	nonceErr  string
	verbCache *verbList
}

// Handler builds the routes.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	sub, _ := fs.Sub(static, "static")
	mux.Handle("GET /", http.FileServer(http.FS(sub)))
	mux.HandleFunc("GET /api/state", s.state)
	mux.HandleFunc("GET /api/events", s.events)
	mux.HandleFunc("GET /api/dirs", s.dirs)
	mux.HandleFunc("GET /api/preview", s.preview)
	mux.HandleFunc("GET /api/panes/{session}/{id}/screen", s.screen)
	mux.HandleFunc("GET /api/panes/{session}/{id}/activity", s.activity)
	mux.HandleFunc("GET /api/panes/{session}/{id}/transcript", s.transcript)
	mux.HandleFunc("GET /api/panes/{session}/{id}/stream", s.stream)
	mux.HandleFunc("POST /api/panes/{session}/{id}/prompt", s.prompt)
	mux.HandleFunc("POST /api/panes/{session}/{id}/capture", s.capture)
	mux.HandleFunc("DELETE /api/panes/{session}/{id}", s.closePane)
	mux.HandleFunc("POST /api/agents", s.startAgent)
	mux.HandleFunc("POST /api/terminals", s.newTerminal)
	mux.HandleFunc("GET /api/projects", s.listProjects)
	mux.HandleFunc("POST /api/projects", s.createProject)
	mux.HandleFunc("PATCH /api/projects/{id}", s.patchProject)
	mux.HandleFunc("DELETE /api/projects/{id}", s.deleteProject)
	mux.HandleFunc("GET /api/tasks", s.listTasks)
	mux.HandleFunc("POST /api/tasks", s.createTask)
	mux.HandleFunc("PATCH /api/tasks/{id}", s.patchTask)
	mux.HandleFunc("DELETE /api/tasks/{id}", s.deleteTask)
	mux.HandleFunc("GET /api/notes", s.listNotes)
	mux.HandleFunc("POST /api/notes", s.createNote)
	mux.HandleFunc("PATCH /api/notes/{id}", s.patchNote)
	mux.HandleFunc("DELETE /api/notes/{id}", s.deleteNote)
	mux.HandleFunc("GET /api/assignments", s.listAssignments)
	mux.HandleFunc("PUT /api/panes/{session}/{id}/assignment", s.putAssignment)
	mux.HandleFunc("DELETE /api/panes/{session}/{id}/assignment", s.deleteAssignment)
	mux.HandleFunc("GET /api/captures", s.captures)
	mux.HandleFunc("GET /api/captures/{name}", s.readCapture)
	mux.HandleFunc("DELETE /api/captures/{name}", s.deleteCapture)
	mux.HandleFunc("GET /api/context", s.listContext)
	mux.HandleFunc("POST /api/panes/{session}/{id}/context/extract", s.extractContext)
	mux.HandleFunc("PATCH /api/context/{id}", s.patchContext)
	mux.HandleFunc("DELETE /api/context/{id}", s.deleteContext)
	mux.HandleFunc("POST /api/context/{id}/insert", s.insertContext)
	mux.HandleFunc("POST /api/context/{id}/open", s.openContext)
	mux.HandleFunc("GET /api/reminders", s.listReminders)
	mux.HandleFunc("POST /api/reminders", s.createReminder)
	mux.HandleFunc("PATCH /api/reminders/{id}", s.patchReminder)
	mux.HandleFunc("DELETE /api/reminders/{id}", s.deleteReminder)
	mux.HandleFunc("GET /api/prompt-presets", s.listPresets)
	mux.HandleFunc("POST /api/prompt-presets", s.createPreset)
	mux.HandleFunc("POST /api/prompt-presets/example", s.createExamplePreset)
	mux.HandleFunc("PATCH /api/prompt-presets/{id}", s.patchPreset)
	mux.HandleFunc("DELETE /api/prompt-presets/{id}", s.deletePreset)
	mux.HandleFunc("GET /api/palette", s.palette)
	mux.HandleFunc("GET /api/verbs", s.listVerbs)
	mux.HandleFunc("POST /api/call", s.call)
	return mux
}

// HoldPresence asks the daemon for the person's nonce and keeps it for the
// life of the server. When the daemon drops the connection, as it does when
// it restarts, the nonce is cleared and asked for again. A refusal from
// inside a tuios pane is final; the transcript then says so.
func (s *Server) HoldPresence(ctx context.Context) {
	go func() {
		for {
			nonce, lost, err := s.Client.Presence(ctx)
			s.mu.Lock()
			if err != nil {
				s.nonce, s.nonceErr = "", err.Error()
			} else {
				s.nonce, s.nonceErr = nonce, ""
			}
			s.mu.Unlock()
			if err != nil {
				var derr *daemon.Error
				if errors.As(err, &derr) && derr.Code == "forbidden" {
					return
				}
			} else {
				select {
				case <-ctx.Done():
					return
				case <-lost:
					s.mu.Lock()
					s.nonce, s.nonceErr = "", "the presence connection ended; asking again"
					s.mu.Unlock()
					Log.Println("presence lost; asking the daemon again")
				}
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(3 * time.Second):
			}
		}
	}()
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, err error) {
	var derr *daemon.Error
	if errors.As(err, &derr) {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": derr.Message, "code": derr.Code, "hint": derr.Hint})
		return
	}
	writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
}

func readJSON(r *http.Request, v any) error {
	return json.NewDecoder(http.MaxBytesReader(nil, r.Body, 1<<20)).Decode(v)
}

func callCtx(r *http.Request) (context.Context, context.CancelFunc) {
	return context.WithTimeout(r.Context(), callTimeout)
}

func (s *Server) state(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := callCtx(r)
	defer cancel()
	sessions, err := panes.List(ctx, s.Client)
	if err != nil {
		writeErr(w, err)
		return
	}
	info, _ := s.Client.Hello(ctx, s.Version)
	s.mu.Lock()
	transcripts := s.nonce != ""
	s.mu.Unlock()
	writeJSON(w, 200, map[string]any{
		"daemon": info, "socket": s.Client.Socket, "sessions": s.decorate(sessions),
		"transcripts": transcripts, "capture_dir": s.CaptureDir,
		"projects": s.projectsJSON(), "tasks": s.tasksJSON(""),
	})
}

// paneView is a pane with what it is assigned to, by name, so the page
// draws a row without a second request.
type paneView struct {
	panes.Pane
	Assignment map[string]any `json:"assignment"`
}

type sessionView struct {
	panes.Session
	Panes []paneView `json:"panes"`
}

// decorate adds each pane's assignment to the listing.
func (s *Server) decorate(sessions []panes.Session) []sessionView {
	out := make([]sessionView, 0, len(sessions))
	for _, se := range sessions {
		v := sessionView{Session: se, Panes: make([]paneView, 0, len(se.Panes))}
		for _, p := range se.Panes {
			v.Panes = append(v.Panes, paneView{Pane: p, Assignment: s.assignmentJSON(p.ID)})
		}
		out = append(out, v)
	}
	return out
}

// events relays the daemon's event stream as server-sent events.
func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", 500)
		return
	}
	ch, err := s.Client.Subscribe(r.Context(), "", []string{
		"agent-state", "attention", "window-created", "window-closed", "window-exit",
		"window-retitled", "session-created", "session-closed",
	})
	if err != nil {
		writeErr(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	fmt.Fprint(w, "event: open\ndata: {}\n\n")
	fl.Flush()
	keep := time.NewTicker(20 * time.Second)
	defer keep.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-keep.C:
			fmt.Fprint(w, ": keep\n\n")
			fl.Flush()
		case ev, ok := <-ch:
			if !ok {
				fmt.Fprint(w, "event: closed\ndata: {}\n\n")
				fl.Flush()
				return
			}
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", ev.Type, strings.TrimSpace(string(ev.Raw)))
			fl.Flush()
		}
	}
}

func (s *Server) screen(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := callCtx(r)
	defer cancel()
	lines, _ := strconv.Atoi(r.URL.Query().Get("lines"))
	if lines <= 0 {
		lines = 300
	}
	c, err := s.Client.CapturePane(ctx, r.PathValue("session"), r.PathValue("id"), lines, false)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, c)
}

func (s *Server) activity(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := callCtx(r)
	defer cancel()
	a, err := s.Client.Activity(ctx, r.PathValue("session"), r.PathValue("id"), 256)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"lines": panes.ActivityLines(a), "recap": a.Recap})
}

func (s *Server) transcript(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	nonce, nonceErr := s.nonce, s.nonceErr
	s.mu.Unlock()
	if nonce == "" {
		msg := "transcript needs the person's presence, which the daemon refused"
		if nonceErr != "" {
			msg += ": " + nonceErr
		}
		writeJSON(w, 200, map[string]any{"lines": []string{msg, "", "Run deck-web outside a tuios pane to read transcripts. Activity works from anywhere."}, "unavailable": true})
		return
	}
	ctx, cancel := callCtx(r)
	defer cancel()
	t, err := s.Client.Transcript(ctx, r.PathValue("session"), r.PathValue("id"), nonce, 400)
	if err != nil {
		var derr *daemon.Error
		if errors.As(err, &derr) && derr.Code == "no_transcript" {
			writeJSON(w, 200, map[string]any{"lines": []string{"(this pane is not joined to an agent transcript)", "", derr.Message}, "unavailable": true})
			return
		}
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"lines": panes.TranscriptLines(t)})
}

func (s *Server) prompt(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Text    string `json:"text"`
		IsAgent bool   `json:"is_agent"`
		Preset  string `json:"preset"`
	}
	if err := readJSON(r, &in); err != nil || strings.TrimSpace(in.Text) == "" {
		writeJSON(w, 400, map[string]any{"error": "text is required"})
		return
	}
	var err error
	if _, in.Text, err = s.applyPreset(in.Preset, "", in.Text); err != nil {
		badRequest(w, err.Error())
		return
	}
	ctx, cancel := callCtx(r)
	defer cancel()
	session, id := r.PathValue("session"), r.PathValue("id")
	if in.IsAgent {
		res, err := s.Client.QueuePrompt(ctx, session, id, in.Text)
		if err != nil {
			writeErr(w, err)
			return
		}
		status := fmt.Sprintf("prompt queued at position %d", res.Position)
		if res.Delivering {
			status = "prompt delivering"
		}
		writeJSON(w, 200, map[string]any{"status": status, "queue": res})
		return
	}
	if err := s.Client.SendText(ctx, session, id, in.Text+"\n"); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"status": "sent"})
}

func (s *Server) capture(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := callCtx(r)
	defer cancel()
	session, id := r.PathValue("session"), r.PathValue("id")
	c, err := s.Client.CapturePane(ctx, session, id, 0, false)
	if err != nil {
		writeErr(w, err)
		return
	}
	name := r.URL.Query().Get("name")
	if name == "" {
		name = panes.ShortID(id)
	}
	path, err := capture.Write(s.CaptureDir, session, name, c.Content)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"status": "captured to " + path, "path": path, "lines": strings.Count(c.Content, "\n") + 1})
}

func (s *Server) closePane(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := callCtx(r)
	defer cancel()
	if err := s.Client.CloseWindow(ctx, r.PathValue("session"), r.PathValue("id")); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"status": "closed"})
}

// startAgent opens an agent pane and, when the request names a task or a
// project, assigns it. The target is resolved before the daemon is asked,
// so a bad one leaves no stray pane.
func (s *Server) startAgent(w http.ResponseWriter, r *http.Request) {
	var in struct {
		daemon.StartAgentParams
		target
		Preset string `json:"preset"`
	}
	if err := readJSON(r, &in); err != nil || strings.TrimSpace(in.Agent) == "" {
		writeJSON(w, 400, map[string]any{"error": "agent is required"})
		return
	}
	taskID, projectID, err := s.resolveNew(in.target)
	if err != nil {
		badRequest(w, err.Error())
		return
	}
	if in.Cwd, in.Prompt, err = s.applyPreset(in.Preset, in.Cwd, in.Prompt); err != nil {
		badRequest(w, err.Error())
		return
	}
	in.Focus = false
	ctx, cancel := context.WithTimeout(r.Context(), 150*time.Second)
	defer cancel()
	res, err := s.Client.StartAgent(ctx, in.StartAgentParams)
	if err != nil {
		writeErr(w, err)
		return
	}
	status := res.Name + " (" + panes.ShortID(res.WindowID) + ")"
	if res.Ready {
		status += " is ready: " + res.State
	} else {
		status += " is not ready: " + res.Reason
	}
	if res.PromptStatus != "" {
		status += "; prompt " + res.PromptStatus
	}
	status = s.assignNew(res.WindowID, res.Session, taskID, projectID, status)
	writeJSON(w, 200, map[string]any{"status": status, "result": res, "assignment": s.assignmentJSON(res.WindowID)})
}

// resolveNew is the target of a new pane. The rule is the TUI's: a task or
// a project is required, and a task implies its project. Until the page
// sends these fields, a request that leaves every one of them out, rather
// than sending them empty, is the old request and makes an unassigned pane;
// that is the pre-page compatibility behaviour, not an exception to the
// rule.
func (s *Server) resolveNew(t target) (taskID, projectID string, err error) {
	if !t.given() {
		return "", "", nil
	}
	return s.resolveTarget(t)
}

// newTerminal opens an interactive shell pane, the pane tuios itself opens,
// so it stays up between the commands sent to it through prompt.
func (s *Server) newTerminal(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Session string `json:"session"`
		Name    string `json:"name"`
		Cwd     string `json:"cwd"`
		Cols    int    `json:"cols"`
		Rows    int    `json:"rows"`
		Preset  string `json:"preset"`
		target
	}
	if err := readJSON(r, &in); err != nil || in.Session == "" {
		writeJSON(w, 400, map[string]any{"error": "session is required"})
		return
	}
	taskID, projectID, err := s.resolveNew(in.target)
	if err != nil {
		badRequest(w, err.Error())
		return
	}
	if in.Cwd, _, err = s.applyPreset(in.Preset, in.Cwd, ""); err != nil {
		badRequest(w, err.Error())
		return
	}
	ctx, cancel := callCtx(r)
	defer cancel()
	sessions, err := s.Client.ListSessions(ctx)
	if err != nil {
		writeErr(w, err)
		return
	}
	exists := false
	for _, se := range sessions {
		if se.Name == in.Session {
			exists = true
		}
	}
	if !exists {
		if err := s.Client.NewSession(ctx, in.Session, in.Cwd, in.Cols, in.Rows); err != nil {
			writeErr(w, err)
			return
		}
	}
	res, err := s.Client.NewWindow(ctx, in.Session, in.Name, in.Cwd, nil)
	if err != nil {
		writeErr(w, err)
		return
	}
	status := s.assignNew(res.WindowID, in.Session, taskID, projectID, "opened "+res.Name+" ("+panes.ShortID(res.WindowID)+") in "+in.Session)
	writeJSON(w, 200, map[string]any{"status": status, "result": res, "assignment": s.assignmentJSON(res.WindowID)})
}

// dirs lists the folders under a path for the directory picker. The browser
// cannot name a folder on this machine itself, so the page browses here.
func (s *Server) dirs(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	if path == "" || strings.HasPrefix(path, "~") {
		home, _ := os.UserHomeDir()
		path = home + strings.TrimPrefix(path, "~")
	}
	path = filepath.Clean(path)
	entries, err := os.ReadDir(path)
	if err != nil {
		writeJSON(w, 400, map[string]any{"error": err.Error(), "path": path})
		return
	}
	names := []string{}
	for _, e := range entries {
		if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	writeJSON(w, 200, map[string]any{"path": path, "parent": filepath.Dir(path), "dirs": names})
}

// Log is the server's logger.
var Log = log.New(os.Stderr, "deck-web: ", log.LstdFlags)
