package web

import (
	"net/http"
	"os/exec"
	"strings"

	ctxitem "github.com/unixsuperhero/tuios-deck/internal/context"
)

// insertContext types an item's text into a pane without a newline, so
// nothing is submitted.
func (s *Server) insertContext(w http.ResponseWriter, r *http.Request) {
	c := s.Store.ContextItem(r.PathValue("id"))
	if c == nil {
		writeJSON(w, 404, map[string]any{"error": "no context item " + r.PathValue("id")})
		return
	}
	var in struct {
		Session string `json:"session"`
		Window  string `json:"window"`
	}
	if err := readJSON(r, &in); err != nil || in.Session == "" || in.Window == "" {
		badRequest(w, "session and window are required")
		return
	}
	ctx, cancel := callCtx(r)
	defer cancel()
	if err := s.Client.SendText(ctx, in.Session, in.Window, c.Text); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"status": "inserted (not submitted)"})
}

// openContext opens a link, a pull request, a file or a folder on the
// machine deck web runs on, with the platform's opener or the program
// given, detached.
func (s *Server) openContext(w http.ResponseWriter, r *http.Request) {
	c := s.Store.ContextItem(r.PathValue("id"))
	if c == nil {
		writeJSON(w, 404, map[string]any{"error": "no context item " + r.PathValue("id")})
		return
	}
	var in struct {
		Program string `json:"program"`
	}
	readJSON(r, &in)
	target, ok := ctxitem.Target(*c)
	if !ok {
		badRequest(w, "a "+c.Kind+" has nothing to open")
		return
	}
	prog := strings.TrimSpace(in.Program)
	if prog == "" {
		prog = ctxitem.Opener()
	}
	if ctxitem.Terminal(prog) {
		badRequest(w, prog+" needs a terminal; open it from deck tui")
		return
	}
	if _, err := exec.LookPath(strings.Fields(prog)[0]); err != nil {
		badRequest(w, "no "+prog+" on PATH")
		return
	}
	if err := ctxitem.Start(prog, target); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"status": "opened with " + prog, "program": prog, "target": target})
}
