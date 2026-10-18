package daemon

import (
	"context"
	"encoding/json"
)

// Session is one row of list-sessions.
type Session struct {
	Name        string          `json:"name"`
	ID          string          `json:"id"`
	Attached    bool            `json:"attached"`
	LastActive  int64           `json:"last_active"`
	WindowCount int             `json:"window_count"`
	Dir         string          `json:"dir"`
	Branch      string          `json:"branch"`
	Windows     []SessionWindow `json:"windows"`
}

// SessionWindow is a window as list-sessions reports it.
type SessionWindow struct {
	ID            string `json:"id"`
	Title         string `json:"title"`
	AgentState    string `json:"agent_state"`
	AgentHarness  string `json:"agent_harness"`
	ForegroundCmd string `json:"foreground_cmd"`
	Workspace     int    `json:"workspace"`
}

// ListSessions lists every session on the daemon with its windows.
func (c *Client) ListSessions(ctx context.Context) ([]Session, error) {
	var out struct {
		Sessions []Session `json:"sessions"`
	}
	err := c.Call(ctx, "list-sessions", map[string]any{}, &out)
	return out.Sessions, err
}

// WindowCwd is the working folder list-windows reports for one window, or
// "" when the window is not in the session.
func (c *Client) WindowCwd(ctx context.Context, session, window string) (string, error) {
	var out struct {
		Windows []struct {
			ID  string `json:"window_id"`
			Cwd string `json:"cwd"`
		} `json:"windows"`
	}
	if err := c.Call(ctx, "list-windows", map[string]any{"session": session}, &out); err != nil {
		return "", err
	}
	for _, w := range out.Windows {
		if w.ID == window {
			return w.Cwd, nil
		}
	}
	return "", nil
}

// Agent is one row of list-agents.
type Agent struct {
	Session      string `json:"session"`
	WindowID     string `json:"window_id"`
	Name         string `json:"name"`
	State        string `json:"state"`
	Message      string `json:"message"`
	AgentStateAt int64  `json:"agent_state_at"`
	HarnessID    string `json:"harness_id"`
	Foreground   string `json:"foreground"`
	Cwd          string `json:"cwd"`
	Ready        bool   `json:"ready"`
	BlockedBy    string `json:"blocked_by"`
	NeedsYou     bool   `json:"needs_you"`
	Queued       int    `json:"queued"`
	Unread       int    `json:"unread"`
}

// ListAgents lists the agent panes of every session.
func (c *Client) ListAgents(ctx context.Context) ([]Agent, error) {
	var out struct {
		Agents []Agent `json:"agents"`
	}
	err := c.Call(ctx, "list-agents", map[string]any{"all_sessions": true}, &out)
	return out.Agents, err
}

// Capture is the result of capture-pane.
type Capture struct {
	Content     string `json:"content"`
	Source      string `json:"source"`
	HistoryRows int    `json:"history_rows"`
	Revision    int64  `json:"revision"`
	BootID      string `json:"boot_id"`
}

// CapturePane reads the last lines of a pane's screen and scrollback.
func (c *Client) CapturePane(ctx context.Context, session, window string, lines int, styled bool) (Capture, error) {
	var out Capture
	p := map[string]any{"session": session, "window": window, "source": "recent", "styled": styled}
	if lines > 0 {
		p["lines"] = lines
	}
	err := c.Call(ctx, "capture-pane", p, &out)
	return out, err
}

// ActivityEntry is one entry of an agent's activity ring.
type ActivityEntry struct {
	Seq    int64    `json:"seq"`
	At     int64    `json:"at"`
	Kind   string   `json:"kind"`
	Tool   string   `json:"tool"`
	Target string   `json:"target"`
	Files  []string `json:"files"`
	OK     *bool    `json:"ok"`
	Exit   *int     `json:"exit"`
	Text   string   `json:"text"`
}

// Activity is the result of agent-activity with recap.
type Activity struct {
	Entries []ActivityEntry `json:"entries"`
	LastSeq int64           `json:"last_seq"`
	Recap   struct {
		Turns      int             `json:"turns"`
		Files      []string        `json:"files"`
		FilesTotal int             `json:"files_total"`
		Commands   int             `json:"commands"`
		Tests      json.RawMessage `json:"tests"`
		LastSaid   string          `json:"last_said"`
		State      string          `json:"state"`
	} `json:"recap"`
}

// Activity reads what the agent in a pane has been doing.
func (c *Client) Activity(ctx context.Context, session, window string, limit int) (Activity, error) {
	var out Activity
	err := c.Call(ctx, "agent-activity", map[string]any{
		"session": session, "window": window, "recap": true, "limit": limit,
	}, &out)
	return out, err
}

// TranscriptEntry is one entry of agent-transcript.
type TranscriptEntry struct {
	ID     string `json:"id"`
	At     int64  `json:"at"`
	Role   string `json:"role"`
	Kind   string `json:"kind"`
	Text   string `json:"text"`
	Tool   string `json:"tool"`
	Target string `json:"target"`
	Status string `json:"status"`
}

// Transcript is the result of agent-transcript.
type Transcript struct {
	Harness string            `json:"harness"`
	Cursor  string            `json:"cursor"`
	Entries []TranscriptEntry `json:"entries"`
}

// Transcript reads the newest entries of an agent's conversation. It needs
// the person's nonce from Presence.
func (c *Client) Transcript(ctx context.Context, session, window, nonce string, limit int) (Transcript, error) {
	var out Transcript
	err := c.Call(ctx, "agent-transcript", map[string]any{
		"session": session, "window": window, "human_nonce": nonce, "limit": limit,
	}, &out)
	return out, err
}

// HistoryEntry is one command of command-history: what a shell in a pane
// ran, read between its OSC 133 marks.
type HistoryEntry struct {
	At         int64  `json:"at"`
	Cmdline    string `json:"cmdline"`
	Cwd        string `json:"cwd"`
	DurationMS int64  `json:"duration_ms"`
	Exit       *int   `json:"exit"`
	Name       string `json:"name"`
	Seq        int64  `json:"seq"`
	Session    string `json:"session"`
	Window     string `json:"window"`
}

// CommandHistory lists the commands a pane's shell ran, newest first. A
// shell that sends no OSC 133 marks reports none.
func (c *Client) CommandHistory(ctx context.Context, session, window string, limit int) ([]HistoryEntry, error) {
	var out struct {
		Commands []HistoryEntry `json:"commands"`
	}
	p := map[string]any{"session": session, "window": window}
	if limit > 0 {
		p["limit"] = limit
	}
	err := c.Call(ctx, "command-history", p, &out)
	return out.Commands, err
}

// StartAgentParams are the inputs of start-agent.
type StartAgentParams struct {
	Session string `json:"session,omitempty"`
	Agent   string `json:"agent"`
	Name    string `json:"name,omitempty"`
	Cwd     string `json:"cwd,omitempty"`
	Prompt  string `json:"prompt,omitempty"`
	Focus   bool   `json:"focus"`
	// ReadyTimeout is how long to wait for the agent's prompt, in
	// milliseconds. Zero takes the daemon's default of two minutes.
	ReadyTimeout int `json:"ready_timeout,omitempty"`
}

// StartAgentResult is what start-agent reports once the agent is ready or not.
type StartAgentResult struct {
	Session        string `json:"session"`
	CreatedSession bool   `json:"created_session"`
	WindowID       string `json:"window_id"`
	Name           string `json:"name"`
	Agent          string `json:"agent"`
	Ready          bool   `json:"ready"`
	State          string `json:"state"`
	Outcome        string `json:"outcome"`
	BlockedBy      string `json:"blocked_by"`
	Reason         string `json:"reason"`
	PromptStatus   string `json:"prompt_status"`
	PromptNote     string `json:"prompt_note"`
}

// StartAgent opens a pane running an agent and waits until it is at its prompt.
func (c *Client) StartAgent(ctx context.Context, p StartAgentParams) (StartAgentResult, error) {
	var out StartAgentResult
	err := c.Call(ctx, "start-agent", p, &out)
	return out, err
}

// NewSession creates an empty session sized like the caller's terminal.
func (c *Client) NewSession(ctx context.Context, name, cwd string, width, height int) error {
	p := map[string]any{"name": name, "window": false}
	if cwd != "" {
		p["cwd"] = cwd
	}
	if width > 0 && height > 0 {
		p["width"], p["height"] = width, height
	}
	return c.Call(ctx, "new-session", p, nil)
}

// NewWindowResult is what new-window reports.
type NewWindowResult struct {
	WindowID string `json:"window_id"`
	Name     string `json:"name"`
}

// NewWindow opens a shell, or a command, in a session without taking focus.
func (c *Client) NewWindow(ctx context.Context, session, name, cwd string, command []string) (NewWindowResult, error) {
	var out NewWindowResult
	p := map[string]any{"session": session, "focus": false}
	if name != "" {
		p["name"] = name
	}
	if cwd != "" {
		p["cwd"] = cwd
	}
	if len(command) > 0 {
		p["command"] = command
	}
	err := c.Call(ctx, "new-window", p, &out)
	return out, err
}

// QueueResult is what queue-prompt reports.
type QueueResult struct {
	ID         string `json:"id"`
	Position   int    `json:"position"`
	Queued     int    `json:"queued"`
	Delivering bool   `json:"delivering"`
}

// QueuePrompt leaves a prompt for an agent; the daemon types it when the agent
// is at rest.
func (c *Client) QueuePrompt(ctx context.Context, session, window, text string) (QueueResult, error) {
	var out QueueResult
	err := c.Call(ctx, "queue-prompt", map[string]any{"session": session, "window": window, "text": text}, &out)
	return out, err
}

// SendText writes text to a pane's PTY verbatim.
func (c *Client) SendText(ctx context.Context, session, window, text string) error {
	return c.Call(ctx, "send-text", map[string]any{"session": session, "window": window, "text": text}, nil)
}

// CloseWindow closes a pane.
func (c *Client) CloseWindow(ctx context.Context, session, window string) error {
	return c.Call(ctx, "close-window", map[string]any{"session": session, "window": window}, nil)
}

// Hello identifies the daemon.
type Hello struct {
	Protocol      int    `json:"protocol"`
	DaemonVersion string `json:"daemon_version"`
	Sessions      int    `json:"sessions"`
	OS            string `json:"os"`
}

// Hello performs the handshake.
func (c *Client) Hello(ctx context.Context, version string) (Hello, error) {
	var out Hello
	err := c.Call(ctx, "hello", map[string]any{"client": "tuios-deck", "version": version, "protocol": 1}, &out)
	return out, err
}

// SendKeys types named keys into a pane through send-keys, which encodes
// each key for the pane's modes (application cursor keys and the rest), so
// a client that already decoded its own keys need not re-encode them.
func (c *Client) SendKeys(ctx context.Context, session, window, keys string) error {
	return c.Call(ctx, "send-keys", map[string]any{"session": session, "window": window, "keys": keys}, nil)
}

// Paste writes text to a pane as a paste: bracketed when the pane's program
// asked for bracketed paste, with control characters other than tab and
// newlines removed.
func (c *Client) Paste(ctx context.Context, session, window, text string) error {
	return c.Call(ctx, "send-text", map[string]any{"session": session, "window": window, "text": text, "paste": true}, nil)
}

// CaptureScreen reads a pane's visible screen with its styling: every row
// of the viewport, ending in the blank rows below the cursor.
func (c *Client) CaptureScreen(ctx context.Context, session, window string) (Capture, error) {
	var out Capture
	err := c.Call(ctx, "capture-pane", map[string]any{"session": session, "window": window, "source": "visible", "styled": true}, &out)
	return out, err
}

// Window is get-window's answer. The cursor fields come only while a tuios
// client is attached to the pane's session; nil otherwise.
type Window struct {
	ID            string `json:"id"`
	Width         int    `json:"width"`
	Height        int    `json:"height"`
	CursorX       *int   `json:"cursor_x"`
	CursorY       *int   `json:"cursor_y"`
	CursorVisible bool   `json:"cursor_visible"`
}

// GetWindow describes one window, with its cursor when the daemon knows it.
func (c *Client) GetWindow(ctx context.Context, session, window string) (Window, error) {
	var out Window
	err := c.Call(ctx, "get-window", map[string]any{"session": session, "window": window}, &out)
	return out, err
}
