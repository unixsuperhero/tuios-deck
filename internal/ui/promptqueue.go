package ui

import (
	"context"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/unixsuperhero/tuios-deck/internal/panes"
	"github.com/unixsuperhero/tuios-deck/internal/prompts"
	"github.com/unixsuperhero/tuios-deck/internal/store"
)

// paneLabel names a pane in the queue form's pane field.
func paneLabel(p panes.Pane) string {
	return p.Session + "/" + p.Name + " (" + panes.ShortID(p.ID) + ")"
}

// paneOptions is every agent pane's label, for completion.
func (m *Model) paneOptions() []string {
	var out []string
	for _, s := range m.merged {
		for _, p := range s.Panes {
			if p.IsAgent {
				out = append(out, paneLabel(p))
			}
		}
	}
	return out
}

// findPaneLabel resolves the text of the pane field to the pane it names.
func (m *Model) findPaneLabel(text string) *panes.Pane {
	text = strings.TrimSpace(text)
	for _, s := range m.merged {
		for _, p := range s.Panes {
			if p.IsAgent && strings.EqualFold(paneLabel(p), text) {
				p := p
				return &p
			}
		}
	}
	return nil
}

// paneLabelPreview is the pane field's preview: the pane the label names.
func (m *Model) paneLabelPreview(opt string) previewSpec {
	p := m.findPaneLabel(opt)
	if p == nil {
		return previewSpec{}
	}
	return m.windowPreview("")(p.ID)
}

// queueTargetValues are the task and project field values for the row
// selected on the tasks page, so queueing from there starts on that task.
func (m *Model) queueTargetValues() (task, project string) {
	if m.page != pageTasks || m.tsel < 0 || m.tsel >= len(m.trows) {
		return "", ""
	}
	r := m.trows[m.tsel]
	if r.Task != nil {
		return m.taskPath(*r.Task), ""
	}
	if r.Project != nil {
		return "", r.Project.Name
	}
	return "", ""
}

// openQueueForm queues a prompt for a task or a project, or edits the
// queued prompt given. The prompt is final when queued, its preset's prefix
// in it, so an edit has no preset field.
func (m *Model) openQueueForm(edit *store.QueuedPrompt) tea.Cmd {
	fromModal := m.mode == modePrompts // the modal comes back when the form is done
	session := m.selectedSession()
	if session == "" {
		session = "deck"
	}
	fields := []field{
		{Label: "task", Keep: true, Options: m.taskOptions, Preview: m.taskPreview, New: "task"},
		{Label: "project", Keep: true, Options: m.projectOptions, Preview: m.projectPreview, New: "project"},
	}
	if edit == nil {
		fields = append(fields, m.presetField("its prefix goes in front of the prompt now, once; its cwd is used when cwd is empty"))
	}
	fields = append(fields,
		field{Label: "agent", Default: "claude", Hint: "started when the prompt runs, unless a pane is chosen"},
		field{Label: "session", Default: session},
		field{Label: "cwd", Keep: true, Hint: "where the agent starts; empty takes the preset's, else the selected pane's · ctrl+o chooses a folder"},
		field{Label: "pane", Keep: true, Options: m.paneOptions, Preview: m.paneLabelPreview, Hint: "send to this running agent instead of starting one; empty starts the agent"},
		field{Label: "prompt", Keep: true, Multi: true},
	)
	idx := map[string]int{}
	for i, f := range fields {
		idx[f.Label] = i
	}
	title := "queue prompt"
	if edit != nil {
		title = "edit queued prompt"
		set := func(label, v string) { fields[idx[label]].Value = v }
		if edit.TaskID != "" {
			if t := m.store.Task(edit.TaskID); t != nil {
				set("task", m.taskPath(*t))
			}
		} else if p := m.store.Project(edit.ProjectID); p != nil {
			set("project", p.Name)
		}
		set("agent", edit.Agent)
		set("session", edit.Session)
		set("cwd", edit.Cwd)
		set("prompt", edit.Text)
		if edit.Window != "" {
			set("pane", "gone: "+panes.ShortID(edit.Window))
			for _, s := range m.merged {
				for _, p := range s.Panes {
					if p.ID == edit.Window {
						set("pane", paneLabel(p))
					}
				}
			}
		}
	} else {
		task, project := m.queueTargetValues()
		if task == "" && project == "" && m.focus != nil {
			target := m.targetFields(true)
			task, project = target[0].Value, target[1].Value
		}
		fields[idx["task"]].Value, fields[idx["project"]].Value = task, project
	}
	sel := m.selected()
	val := func(v []string, label string) string { return v[idx[label]] }
	// build resolves the form into the prompt to store, or says which field
	// is wrong.
	build := func(v []string) (store.QueuedPrompt, int, string) {
		taskID, projectID, f, msg := m.resolveTarget(val(v, "task"), val(v, "project"), idx["task"], idx["project"], false)
		if msg != "" {
			return store.QueuedPrompt{}, f, msg
		}
		var pr *store.Preset
		if i, ok := idx["preset"]; ok {
			if pr, msg = m.resolvePreset(v[i]); msg != "" {
				return store.QueuedPrompt{}, i, msg
			}
		}
		if strings.TrimSpace(val(v, "prompt")) == "" {
			return store.QueuedPrompt{}, idx["prompt"], "a queued prompt needs text"
		}
		q := store.QueuedPrompt{
			TaskID: taskID, ProjectID: projectID,
			Agent: val(v, "agent"), Session: val(v, "session"),
			Cwd:  presetCwd(val(v, "cwd"), pr, m.queueBasePane(val(v, "pane"), sel)),
			Text: val(v, "prompt"),
		}
		if pr != nil {
			q.Preset = pr.Name
			q.Text = presetPrefix(pr, q.Text)
		}
		if p := strings.TrimSpace(val(v, "pane")); p != "" {
			pane := m.findPaneLabel(p)
			if pane == nil {
				return store.QueuedPrompt{}, idx["pane"], "no running agent " + p + " (tab lists them); clear the field to start an agent"
			}
			q.Session, q.Window = pane.Session, pane.ID
		}
		return q, 0, ""
	}
	m.mode = modeForm
	m.form = &form{
		Title:  title,
		Fields: fields,
		Validate: func(v []string) (int, string) {
			_, f, msg := build(v)
			return f, msg
		},
		Done: func(v []string) tea.Cmd {
			q, _, _ := build(v)
			return func() tea.Msg {
				if edit != nil {
					q.ID, q.Preset = edit.ID, edit.Preset
					if err := m.store.UpdateQueued(q); err != nil {
						return modalMsg{queue: true, reopen: true, err: err}
					}
					return modalMsg{queue: true, reopen: true, status: "saved the queued prompt"}
				}
				saved, err := m.store.QueuePrompt(q)
				if err != nil {
					return modalMsg{queue: true, reopen: fromModal, err: err}
				}
				return modalMsg{queue: true, reopen: fromModal, status: "queued " + saved.ID + " for " + m.queueTarget(saved)}
			}
		},
	}
	m.cwdKey(m.form, "cwd")
	f := m.form
	// The prompt takes @ files and slash commands of the harness that will
	// read it, under the folder it will start in: the chosen pane's harness
	// and cwd, else the agent field's and the explicit > preset > default cwd.
	live := func(label string) string {
		if i, ok := idx[label]; ok {
			return f.Fields[i].result()
		}
		return ""
	}
	f.cwdFn = func() string {
		var pr *store.Preset
		if i, ok := idx["preset"]; ok {
			pr, _ = m.resolvePreset(f.Fields[i].Value)
		}
		return expandHome(presetCwd(live("cwd"), pr, m.queueBasePane(live("pane"), sel)))
	}
	f.enableAttach(idx["prompt"], func() string {
		if p := m.findPaneLabel(live("pane")); p != nil {
			return p.Harness
		}
		return strings.TrimSpace(live("agent"))
	})
	return nil
}

// queueBasePane is the pane whose cwd a queued prompt falls back to: the
// running agent the pane field names, else the pane selected on the deck.
func (m *Model) queueBasePane(paneText string, sel *panes.Pane) *panes.Pane {
	if p := m.findPaneLabel(paneText); p != nil {
		return p
	}
	return sel
}

// queueTarget names the task or the project a queued prompt belongs to.
func (m *Model) queueTarget(q store.QueuedPrompt) string {
	if q.TaskID != "" {
		if t := m.store.Task(q.TaskID); t != nil {
			return m.taskPath(*t)
		}
		return q.TaskID
	}
	if p := m.store.Project(q.ProjectID); p != nil {
		return p.Name
	}
	return q.ProjectID
}

func (m *Model) openQueueModal() tea.Cmd {
	m.mode = modePrompts
	m.pm = &promptModal{queue: true}
	return nil
}

func (m *Model) queueRows() []string {
	var out []string
	for _, q := range m.store.Queued() {
		how := q.Agent + "@" + q.Session
		if q.Window != "" {
			how = "pane " + panes.ShortID(q.Window)
		}
		out = append(out, m.queueTarget(q)+"  · "+how+"  · "+panes.OneLine(q.Text))
	}
	return out
}

// handleQueueKey answers the queue modal's own keys.
func (m *Model) handleQueueKey(key string, ids []string) tea.Cmd {
	if key == "n" {
		return m.openQueueForm(nil)
	}
	if len(ids) == 0 || m.pm.busy {
		return nil
	}
	q := m.store.QueuedPrompt(ids[m.pm.sel])
	if q == nil {
		return nil
	}
	switch key {
	case "e":
		return m.openQueueForm(q)
	case "d":
		id := q.ID
		m.mode = modeConfirm
		m.confirm.Text = "delete the queued prompt for " + m.queueTarget(*q) + "? (Y/n)"
		m.confirm.Do = func() tea.Msg {
			if err := m.store.DequeuePrompt(id); err != nil {
				return modalMsg{queue: true, reopen: true, err: err}
			}
			return modalMsg{queue: true, reopen: true, status: "deleted the queued prompt"}
		}
	case "enter", "s":
		m.pm.busy = true
		m.setStatus("starting the queued prompt for " + m.queueTarget(*q) + "…")
		return m.startQueued(*q)
	}
	return nil
}

// startQueued sends the queued prompt. Dequeuing is prompts.Dispatch's
// doing and happens only when the prompt went.
func (m *Model) startQueued(q store.QueuedPrompt) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), prompts.StartTimeout)
		defer cancel()
		status, err := prompts.Dispatch(ctx, m.client, m.store, q)
		return modalMsg{queue: true, done: true, status: status, err: err}
	}
}
