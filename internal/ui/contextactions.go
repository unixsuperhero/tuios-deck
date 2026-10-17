package ui

import (
	"context"
	"errors"
	"os/exec"
	"strings"

	tea "charm.land/bubbletea/v2"

	ctxitem "github.com/unixsuperhero/tuios-deck/internal/context"
	"github.com/unixsuperhero/tuios-deck/internal/store"
)

// copyItem puts the item's text on the clipboard: OSC 52 through the
// terminal always, since that reaches the person's clipboard over ssh and
// through tuios, and the machine's own clipboard tool too when one is on
// PATH. The status names what was used.
func (m *Model) copyItem(c store.ContextItem) tea.Cmd {
	used := []string{"OSC 52"}
	if tool, err := ctxitem.Copy(c.Text); err == nil {
		used = append(used, tool)
	} else if !errors.Is(err, ctxitem.ErrNoTool) {
		m.setStatus("error: " + err.Error())
		return tea.SetClipboard(c.Text)
	}
	m.setStatus("copied " + shortText(c.Text) + " with " + strings.Join(used, " and "))
	return tea.SetClipboard(c.Text)
}

// insertItem types the item's text into the pane selected on the deck page
// without a newline, so nothing is submitted: the person reads it there and
// presses enter. An agent gets it typed too, since a queued prompt would be
// sent on its own.
func (m *Model) insertItem(c store.ContextItem) tea.Cmd {
	p := m.selected()
	if p == nil {
		m.setStatus("select a pane on the deck page first")
		return nil
	}
	session, id, name, text := p.Session, p.ID, p.Name, c.Text
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
		defer cancel()
		if err := m.client.SendText(ctx, session, id, text); err != nil {
			return actionMsg{err: err}
		}
		return actionMsg{status: "inserted " + shortText(text) + " into " + name + " (not submitted)"}
	}
}

// openItem opens a link, a pull request, a file or a folder with the
// platform's opener.
func (m *Model) openItem(c store.ContextItem) tea.Cmd {
	target, ok := ctxitem.Target(c)
	if !ok {
		m.setStatus("a " + c.Kind + " has nothing to open; y copies it, enter inserts it")
		return nil
	}
	opener := ctxitem.Opener()
	if err := ctxitem.Start(opener, target); err != nil {
		m.setStatus("error: " + err.Error())
		return nil
	}
	m.setStatus("opened " + shortText(target) + " with " + opener)
	return nil
}

// openWithForm asks which program opens the item. The options are the
// common ones found on PATH. A terminal program, such as vim or less, runs
// in a new terminal pane of the selected pane's session, since it needs a
// terminal; any other runs detached.
func (m *Model) openWithForm(c store.ContextItem) tea.Cmd {
	target, ok := ctxitem.Target(c)
	if !ok {
		m.setStatus("a " + c.Kind + " has nothing to open; y copies it, enter inserts it")
		return nil
	}
	programs := ctxitem.Programs()
	def := ""
	if len(programs) > 0 {
		def = programs[0]
	}
	m.mode = modeForm
	m.form = &form{
		Title:  "open " + shortText(target) + " with",
		Fields: []field{{Label: "program", Default: def, Options: func() []string { return programs }, Hint: "found on PATH: " + strings.Join(programs, " ")}},
		Validate: func(v []string) (int, string) {
			if v[0] == "" {
				return 0, "a program is required"
			}
			if _, err := exec.LookPath(strings.Fields(v[0])[0]); err != nil {
				return 0, "no " + v[0] + " on PATH"
			}
			return 0, ""
		},
		Done: func(v []string) tea.Cmd {
			prog := v[0]
			if ctxitem.Terminal(prog) {
				return m.openInTerminal(prog, target, c.TaskID, c.ProjectID)
			}
			if err := ctxitem.Start(prog, target); err != nil {
				return func() tea.Msg { return actionMsg{err: err} }
			}
			m.setStatus("opened " + shortText(target) + " with " + prog)
			return nil
		},
	}
	return nil
}

// openInTerminal opens a shell pane next to the selected pane and runs the
// program on the target in it, so the pane stays a shell after it quits.
// The pane is assigned to the item's task or project when it has one.
func (m *Model) openInTerminal(prog, target, taskID, projectID string) tea.Cmd {
	session, cwd := m.selectedSession(), cwdDefault(m.selected())
	if session == "" {
		session = "deck"
	}
	line := prog + " " + ctxitem.ShellQuote(target) + "\n"
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
		defer cancel()
		res, err := m.client.NewWindow(ctx, session, prog, cwd, nil)
		if err != nil {
			return actionMsg{err: err}
		}
		if err := m.client.SendText(ctx, session, res.WindowID, line); err != nil {
			return actionMsg{err: err}
		}
		if taskID != "" {
			projectID = ""
		}
		return m.assign(res.WindowID, session, taskID, projectID, "opened "+shortText(target)+" with "+prog+" in "+session+" › "+res.Name)
	}
}
