package web

import (
	"encoding/json"
	"net/http"
	"strings"
)

// verbList is the daemon's list-verbs answer, kept whole for the page and
// indexed by verb name so a call can be refused before it reaches the daemon.
type verbList struct {
	raw   json.RawMessage
	names map[string]bool
}

// verbs is the daemon's verb list, asked for once per server run and kept.
// A failed ask is not kept, so the next request tries again.
func (s *Server) verbs(r *http.Request) (*verbList, error) {
	s.mu.Lock()
	cached := s.verbCache
	s.mu.Unlock()
	if cached != nil {
		return cached, nil
	}
	ctx, cancel := callCtx(r)
	defer cancel()
	var raw json.RawMessage
	if err := s.Client.Call(ctx, "list-verbs", map[string]any{}, &raw); err != nil {
		return nil, err
	}
	var parsed struct {
		Verbs []struct {
			Verb string `json:"verb"`
		} `json:"verbs"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, err
	}
	list := &verbList{raw: raw, names: map[string]bool{}}
	for _, v := range parsed.Verbs {
		list.names[v.Verb] = true
	}
	s.mu.Lock()
	s.verbCache = list
	s.mu.Unlock()
	return list, nil
}

// listVerbs passes the daemon's list-verbs through to the page.
func (s *Server) listVerbs(w http.ResponseWriter, r *http.Request) {
	list, err := s.verbs(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(200)
	w.Write(list.raw)
}

// call runs one daemon verb for the command palette. Only a verb the
// daemon listed is sent; the params go through as the page typed them.
func (s *Server) call(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Verb   string         `json:"verb"`
		Params map[string]any `json:"params"`
	}
	if err := readJSON(r, &in); err != nil {
		badRequest(w, "bad json: "+err.Error())
		return
	}
	in.Verb = strings.TrimSpace(in.Verb)
	if in.Verb == "" {
		badRequest(w, "verb is required")
		return
	}
	list, err := s.verbs(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	if !list.names[in.Verb] {
		writeJSON(w, 400, map[string]any{"error": "unknown verb " + in.Verb, "code": "unknown_verb"})
		return
	}
	if in.Params == nil {
		in.Params = map[string]any{}
	}
	ctx, cancel := callCtx(r)
	defer cancel()
	var result json.RawMessage
	if err := s.Client.Call(ctx, in.Verb, in.Params, &result); err != nil {
		writeErr(w, err)
		return
	}
	if len(result) == 0 {
		result = json.RawMessage("null")
	}
	writeJSON(w, 200, map[string]any{"verb": in.Verb, "result": result})
}
