// Package panes builds the deck's view of the daemon: every session with its
// panes, each pane carrying what list-agents knows of it. Both front ends,
// the TUI and the web server, read this one shape.
package panes

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/unixsuperhero/tuios-deck/internal/daemon"
	"github.com/unixsuperhero/tuios-deck/internal/store"
)

// Pane is one window of one session.
type Pane struct {
	Session   string `json:"session"`
	ID        string `json:"id"`
	Name      string `json:"name"`
	State     string `json:"state"`
	Harness   string `json:"harness"`
	Fg        string `json:"foreground"`
	Cwd       string `json:"cwd"`
	Message   string `json:"message"`
	BlockedBy string `json:"blocked_by"`
	IsAgent   bool   `json:"is_agent"`
	Ready     bool   `json:"ready"`
	NeedsYou  bool   `json:"needs_you"`
	Queued    int    `json:"queued"`
	Unread    int    `json:"unread"`
	Working   bool   `json:"working"`
}

// Session is one session with its panes in window order.
type Session struct {
	Name     string `json:"name"`
	Attached bool   `json:"attached"`
	Dir      string `json:"dir"`
	Branch   string `json:"branch"`
	Panes    []Pane `json:"panes"`
}

// List reads the sessions and the agents and merges them.
func List(ctx context.Context, c *daemon.Client) ([]Session, error) {
	sessions, err := c.ListSessions(ctx)
	if err != nil {
		return nil, err
	}
	agents, err := c.ListAgents(ctx)
	if err != nil {
		return nil, err
	}
	out := Merge(sessions, agents)
	for i := range out {
		var windows struct {
			Windows []struct {
				ID            string `json:"window_id"`
				AtPrompt      *bool  `json:"at_prompt"`
				MarksCommands bool   `json:"marks_commands"`
			} `json:"windows"`
		}
		if err := c.Call(ctx, "list-windows", map[string]any{"session": out[i].Name}, &windows); err != nil {
			return nil, err
		}
		for j := range out[i].Panes {
			p := &out[i].Panes[j]
			if p.IsAgent {
				continue
			}
			for _, w := range windows.Windows {
				if w.ID == p.ID {
					p.Working = w.MarksCommands && w.AtPrompt != nil && !*w.AtPrompt
					break
				}
			}
		}
	}
	return out, nil
}

// Merge joins list-sessions with list-agents by window id.
func Merge(sessions []daemon.Session, agents []daemon.Agent) []Session {
	byWindow := map[string]daemon.Agent{}
	for _, a := range agents {
		byWindow[a.WindowID] = a
	}
	sort.Slice(sessions, func(i, j int) bool { return sessions[i].Name < sessions[j].Name })
	out := make([]Session, 0, len(sessions))
	for _, s := range sessions {
		sess := Session{Name: s.Name, Attached: s.Attached, Dir: s.Dir, Branch: s.Branch}
		for _, w := range s.Windows {
			p := Pane{
				Session: s.Name, ID: w.ID, Name: w.Title, State: w.AgentState,
				Harness: w.AgentHarness, Fg: w.ForegroundCmd,
			}
			if a, ok := byWindow[w.ID]; ok {
				p.IsAgent = true
				if a.Name != "" {
					p.Name = a.Name
				}
				p.State, p.Harness, p.Fg, p.Cwd = a.State, a.HarnessID, a.Foreground, a.Cwd
				p.Message, p.BlockedBy = a.Message, a.BlockedBy
				p.Ready, p.NeedsYou, p.Queued, p.Unread = a.Ready, a.NeedsYou, a.Queued, a.Unread
				p.Working = p.State == "working" && !p.NeedsYou
			}
			if p.Name == "" {
				p.Name = ShortID(w.ID)
			}
			sess.Panes = append(sess.Panes, p)
		}
		out = append(out, sess)
	}
	return out
}

// HiddenTasks returns rail-only exclusions and the next snooze expiry.
// An unknown or idle pane keeps its task visible; a task with no panes does too.
func HiddenTasks(sessions []Session, s *store.Store, now int64) (map[string]bool, int64) {
	assignments := s.Assignments()
	busy := map[string]bool{}
	for _, session := range sessions {
		for _, p := range session.Panes {
			id := assignments[p.ID].TaskID
			if id == "" {
				continue
			}
			all, seen := busy[id]
			busy[id] = p.Working && (!seen || all)
		}
	}
	hidden := map[string]bool{}
	var wake int64
	for _, t := range s.Tasks() {
		hidden[t.ID] = busy[t.ID]
		for _, ancestor := range s.Path(t.ID) {
			if ancestor.SnoozedUntil > now {
				hidden[t.ID] = true
				if wake == 0 || ancestor.SnoozedUntil < wake {
					wake = ancestor.SnoozedUntil
				}
			}
		}
	}
	return hidden, wake
}

// ShortID is the first eight characters of a window id.
func ShortID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

// ActivityLines renders agent-activity as text lines, newest last.
func ActivityLines(a daemon.Activity) []string {
	var out []string
	r := a.Recap
	out = append(out, fmt.Sprintf("state %s · turns %d · commands %d · files %d", r.State, r.Turns, r.Commands, r.FilesTotal))
	if r.LastSaid != "" {
		out = append(out, "last said: "+OneLine(r.LastSaid))
	}
	if len(r.Files) > 0 {
		out = append(out, "files: "+strings.Join(r.Files, ", "))
	}
	out = append(out, "")
	if len(a.Entries) == 0 {
		out = append(out, "(no activity yet: the pane reports no hook events)")
	}
	for _, e := range a.Entries {
		ts := time.Unix(0, e.At).Format("15:04:05")
		var line string
		switch e.Kind {
		case "prompt":
			line = "you> " + OneLine(e.Text)
		case "tool":
			line = "→ " + e.Tool + " " + OneLine(e.Target)
		case "tool_done":
			line = "  ok " + e.Tool
		case "tool_failed":
			line = "  FAILED " + e.Tool + " " + OneLine(e.Text)
		case "turn_end":
			line = "■ turn ended " + OneLine(e.Text)
		case "command":
			line = "$ " + OneLine(e.Target)
			if e.Exit != nil {
				line += fmt.Sprintf(" (exit %d)", *e.Exit)
			}
		default:
			line = e.Kind + " " + OneLine(e.Text)
		}
		out = append(out, ts+" "+line)
	}
	return out
}

// TranscriptLines renders agent-transcript as text lines.
func TranscriptLines(t daemon.Transcript) []string {
	var out []string
	for _, e := range t.Entries {
		ts := time.UnixMilli(e.At).Format("15:04:05")
		switch e.Kind {
		case "tool_call":
			out = append(out, fmt.Sprintf("%s → %s %s", ts, e.Tool, OneLine(e.Target)))
		case "tool_result":
			if e.Status == "error" {
				out = append(out, fmt.Sprintf("%s   FAILED %s", ts, OneLine(e.Text)))
			}
		case "thinking":
			continue
		default:
			prefix := e.Role + ">"
			if e.Role == "user" {
				prefix = "you>"
			}
			first := true
			for _, l := range strings.Split(strings.TrimRight(e.Text, "\n"), "\n") {
				if first {
					out = append(out, ts+" "+prefix+" "+l)
					first = false
				} else {
					out = append(out, "         "+l)
				}
			}
			out = append(out, "")
		}
	}
	if len(out) == 0 {
		out = append(out, "(empty transcript for harness "+t.Harness+")")
	}
	return out
}

// OneLine flattens text to one line of at most 200 characters.
func OneLine(s string) string {
	s = strings.ReplaceAll(strings.TrimSpace(s), "\n", " ")
	if len(s) > 200 {
		s = s[:200] + "…"
	}
	return s
}
