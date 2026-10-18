package ui

import (
	"context"
	"os/exec"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/unixsuperhero/tuios-deck/internal/panes"
)

// A vim pane is a shell pane running an editor of the vi family. The deck
// opens one the way it opens a terminal, then types the editor's name into
// the shell once it has drawn its prompt, so the pane is an interactive
// shell again when the editor exits.

// editorOptions is the editors on PATH, nvim first.
func editorOptions() []string {
	var out []string
	for _, e := range panes.Editors {
		if _, err := exec.LookPath(e); err == nil {
			out = append(out, e)
		}
	}
	return out
}

// openEditorForm opens an editor in a new pane: session, name, cwd, task
// or project, and which editor. The defaults are the active pair's and
// its folder, like the new terminal form's.
func (m *Model) openEditorForm() tea.Cmd {
	p := m.selected()
	session := m.selectedSession()
	if session == "" {
		session = "deck"
	}
	editors := editorOptions()
	editor := "nvim"
	if len(editors) > 0 {
		editor = editors[0]
	}
	fields := []field{
		{Label: "session", Default: session},
		{Label: "name", Keep: true, Hint: "empty takes the editor's name"},
		{Label: "cwd", Keep: true},
	}
	fields = append(fields, m.targetFields(false)...)
	fields = append(fields, field{Label: "editor", Default: editor, Options: func() []string { return editors }, Hint: "typed into the new shell once it shows its prompt"})
	m.mode = modeForm
	m.form = &form{
		Title:  "new editor",
		Fields: fields,
		Validate: func(v []string) (int, string) {
			if strings.ContainsAny(v[5], " \t\n") || v[5] == "" {
				return 5, "the editor is one program name"
			}
			_, _, f, msg := m.resolveTarget(v[3], v[4], 3, 4, false)
			return f, msg
		},
		Done: func(v []string) tea.Cmd {
			taskID, projectID, _, _ := m.resolveTarget(v[3], v[4], 3, 4, false)
			name := v[1]
			if name == "" {
				name = v[5]
			}
			return m.newEditor(v[0], name, m.startDir(v[2], nil, v[3], v[4], p), v[5], taskID, projectID)
		},
	}
	f := m.form
	f.Fields[2].HintFn = func() string {
		return "empty starts in " + m.startDir("", nil, f.Fields[3].result(), f.Fields[4].result(), p) + " · ctrl+o chooses a folder"
	}
	m.cwdKey(f, "cwd")
	return nil
}

// newEditor opens a shell pane like newTerminal, waits for the shell to
// print its prompt, and types the editor's name and enter into it. The
// window is assigned to the task or project the form gave.
func (m *Model) newEditor(session, name, cwd, editor, taskID, projectID string) tea.Cmd {
	width, height := m.width, m.height
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
		defer cancel()
		sessions, err := m.client.ListSessions(ctx)
		if err != nil {
			return actionMsg{err: err}
		}
		exists := false
		for _, s := range sessions {
			if s.Name == session {
				exists = true
			}
		}
		if !exists {
			if err := m.client.NewSession(ctx, session, cwd, width, height); err != nil {
				return actionMsg{err: err}
			}
		}
		res, err := m.client.NewWindow(ctx, session, name, cwd, nil)
		if err != nil {
			return actionMsg{err: err}
		}
		// The shell has drawn something once it is ready to read; a shell
		// that prints nothing within the timeout gets the text anyway.
		m.client.Call(ctx, "wait-for", map[string]any{"condition": "window-output", "session": session, "window": res.WindowID, "pattern": `\S`, "source": "visible", "timeout": 5000}, nil)
		if err := m.client.SendText(ctx, session, res.WindowID, editor+"\n"); err != nil {
			return actionMsg{err: err}
		}
		return m.assign(res.WindowID, session, taskID, projectID, "opened "+editor+" in "+res.Name+" ("+panes.ShortID(res.WindowID)+") in "+session)
	}
}
