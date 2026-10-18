// Package prompts sends a queued prompt, for both front ends, and decides
// whether it went. A prompt leaves the queue only when it did.
package prompts

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/unixsuperhero/tuios-deck/internal/daemon"
	"github.com/unixsuperhero/tuios-deck/internal/panes"
	"github.com/unixsuperhero/tuios-deck/internal/store"
)

// StartTimeout is how long starting an agent may take: it waits for the
// agent's prompt.
const StartTimeout = 150 * time.Second

// Dispatch sends the queued prompt and takes it off the queue.
//
// With a Window it is left for that pane, which the daemon types when the
// agent is at rest. Without one an agent is started in Session at Cwd with
// the prompt, and the new pane is assigned to the prompt's task or project.
//
// An error leaves the prompt queued. A started agent counts only when it is
// ready and the daemon says the prompt was sent: a stalled or held prompt
// may already sit in the pane, so the error names the pane to look at and
// the prompt stays for the person to decide.
func Dispatch(ctx context.Context, c *daemon.Client, st *store.Store, q store.QueuedPrompt) (string, error) {
	if q.Window != "" {
		res, err := c.QueuePrompt(ctx, q.Session, q.Window, q.Text)
		if err != nil {
			return "", fmt.Errorf("not sent, kept in the queue: %w", err)
		}
		warning := st.RecordSubmission(q.Session, q.Window, q.Text, "queued")
		if err := st.DequeuePrompt(q.ID); err != nil {
			return "", err
		}
		if res.Delivering {
			return "prompt delivering to " + q.Window + warning, nil
		}
		return fmt.Sprintf("prompt queued at position %d in %s", res.Position, q.Window) + warning, nil
	}
	if strings.TrimSpace(q.Agent) == "" {
		return "", errors.New("not sent, kept in the queue: no agent to start")
	}
	res, err := c.StartAgent(ctx, daemon.StartAgentParams{
		Session: q.Session, Agent: q.Agent, Cwd: q.Cwd, Prompt: q.Text,
	})
	if err != nil {
		return "", fmt.Errorf("not started, kept in the queue: %w", err)
	}
	where := res.Name + " (" + panes.ShortID(res.WindowID) + ")"
	if res.Session == "" {
		res.Session = q.Session
	}
	assigned := ""
	if res.WindowID != "" {
		if err := st.Assign(res.WindowID, res.Session, q.TaskID, q.ProjectID); err != nil {
			assigned = "; not assigned: " + err.Error()
		}
	}
	assigned += st.RecordSubmission(res.Session, res.WindowID, q.Text, res.PromptStatus)
	switch {
	case !res.Ready:
		return "", fmt.Errorf("%s is not ready (%s); the prompt is kept in the queue, check the pane before starting it again%s", where, res.Reason, assigned)
	case res.PromptStatus != "sent":
		return "", fmt.Errorf("%s: prompt %s %s; the prompt is kept in the queue, check the pane before starting it again%s", where, res.PromptStatus, res.PromptNote, assigned)
	}
	if err := st.DequeuePrompt(q.ID); err != nil {
		return "", err
	}
	return where + " started and the prompt was sent" + assigned, nil
}
