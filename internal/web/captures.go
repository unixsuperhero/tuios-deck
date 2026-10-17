package web

import (
	"errors"
	"net/http"
	"os"
	"strings"

	"github.com/unixsuperhero/tuios-deck/internal/capture"
)

// captures lists the capture files, newest first.
func (s *Server) captures(w http.ResponseWriter, r *http.Request) {
	list, err := capture.List(s.CaptureDir)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"dir": s.CaptureDir, "captures": list})
}

// readCapture is one capture's content.
func (s *Server) readCapture(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	content, err := capture.Read(s.CaptureDir, name)
	if err != nil {
		captureErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"name": name, "content": content, "lines": strings.Count(content, "\n") + 1})
}

// deleteCapture removes one capture file.
func (s *Server) deleteCapture(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if err := capture.Remove(s.CaptureDir, name); err != nil {
		captureErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"status": "deleted " + name, "name": name})
}

func captureErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, capture.ErrName):
		writeJSON(w, 400, map[string]any{"error": err.Error()})
	case errors.Is(err, os.ErrNotExist):
		writeJSON(w, 404, map[string]any{"error": "no such capture"})
	default:
		writeErr(w, err)
	}
}
