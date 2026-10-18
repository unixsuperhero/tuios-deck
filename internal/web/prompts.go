package web

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/unixsuperhero/tuios-deck/internal/store"
)

// listPresets is every prompt preset.
func (s *Server) listPresets(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{"presets": s.Store.Presets()})
}

type presetIn struct {
	Name   string `json:"name"`
	Prefix string `json:"prefix"`
	Cwd    string `json:"cwd"`
}

func (s *Server) createPreset(w http.ResponseWriter, r *http.Request) {
	var in presetIn
	if err := readJSON(r, &in); err != nil {
		badRequest(w, "bad json")
		return
	}
	p, err := s.Store.AddPreset(in.Name, in.Prefix, in.Cwd)
	if err != nil {
		badRequest(w, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"status": "added preset " + p.Name, "preset": p})
}

// createExamplePreset adds the short-answer example. Nothing adds it but
// this explicit request.
func (s *Server) createExamplePreset(w http.ResponseWriter, r *http.Request) {
	ex := store.ExamplePreset()
	p, err := s.Store.AddPreset(ex.Name, ex.Prefix, ex.Cwd)
	if err != nil {
		badRequest(w, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"status": "added preset " + p.Name, "preset": p})
}

func (s *Server) patchPreset(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	p := s.Store.Preset(id)
	if p == nil {
		writeJSON(w, 404, map[string]any{"error": "no preset " + id})
		return
	}
	var in struct {
		Name   *string `json:"name"`
		Prefix *string `json:"prefix"`
		Cwd    *string `json:"cwd"`
	}
	if err := readJSON(r, &in); err != nil {
		badRequest(w, "bad json")
		return
	}
	if in.Name != nil {
		p.Name = *in.Name
	}
	if in.Prefix != nil {
		p.Prefix = *in.Prefix
	}
	if in.Cwd != nil {
		p.Cwd = *in.Cwd
	}
	if err := s.Store.UpdatePreset(id, p.Name, p.Prefix, p.Cwd); err != nil {
		badRequest(w, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"status": "saved preset " + p.Name, "preset": s.Store.Preset(id)})
}

func (s *Server) deletePreset(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.Store.DeletePreset(id); err != nil {
		writeJSON(w, 404, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]any{"status": "deleted preset", "id": id})
}

// applyPreset is the effective cwd and prompt for a request that names a
// preset: its prefix goes in front of a non-empty prompt once, and its cwd
// stands in for an empty one. No preset leaves both as they are.
func (s *Server) applyPreset(name, cwd, prompt string) (string, string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return cwd, prompt, nil
	}
	p := s.Store.PresetByName(name)
	if p == nil {
		return "", "", fmt.Errorf("no preset %s", name)
	}
	if strings.TrimSpace(cwd) == "" {
		cwd = p.Cwd
	}
	if strings.TrimSpace(prompt) != "" {
		prompt = store.ApplyPrefix(p.Prefix, prompt)
	}
	return cwd, prompt, nil
}
