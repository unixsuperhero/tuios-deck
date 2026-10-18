package daemon

import "context"

// Notice leaves a message in a session's agent ring as a notice everyone in
// the session can read. An empty session is the most recently active one.
func (c *Client) Notice(ctx context.Context, session, subject, text string) error {
	p := map[string]any{"subject": subject, "text": text}
	if session != "" {
		p["session"] = session
	}
	return c.Call(ctx, "send-agent-message", p, nil)
}
