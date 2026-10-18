// Package history persists the daemon's retained activity and live lifecycle
// events. It runs beside the front end and never changes its selection or scroll.
package history

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/unixsuperhero/tuios-deck/internal/daemon"
	"github.com/unixsuperhero/tuios-deck/internal/panes"
	"github.com/unixsuperhero/tuios-deck/internal/store"
)

const Boundary = "History keeps deck submission acknowledgements, observed events and retained agent activity / shell commands. Queue acceptance is not delivery. Offline gaps, expired rings and unreported prompts cannot be recovered. A turn-end note is not necessarily the full response."

var eventTypes = []string{"agent-activity", "command-started", "command-finished", "window-created", "window-closed", "window-exit", "window-retitled", "window-moved", "window-minimized", "window-restored", "session-created", "session-closed", "gap"}

// Run subscribes before recovering retained sources, so recovery cannot miss
// events that arrive while the sources are being read. Stable IDs deduplicate both.
func Run(ctx context.Context, client *daemon.Client, st *store.Store) {
	for ctx.Err() == nil {
		err := watch(ctx, client, st)
		if ctx.Err() != nil {
			return
		}
		st.SetHistoryError("history observation interrupted: " + err.Error())
		select {
		case <-ctx.Done():
			return
		case <-time.After(3 * time.Second):
		}
	}
}

func watch(ctx context.Context, client *daemon.Client, st *store.Store) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	events, err := client.Subscribe(ctx, "", eventTypes)
	if err != nil {
		return err
	}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case ev, ok := <-events:
			if !ok {
				return fmt.Errorf("daemon stream closed; retained sources will be read on reconnect")
			}
			if ev.Type == "subscribed" {
				err = recoverSources(ctx, client, st, ev.BootID)
			} else {
				err = observe(ctx, client, st, ev)
			}
			if err != nil {
				return err
			}
			st.SetHistoryError("")
		}
	}
}

func recoverSources(ctx context.Context, client *daemon.Client, st *store.Store, boot string) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	sessions, err := panes.List(ctx, client)
	if err != nil {
		return err
	}
	var entries []store.HistoryEntry
	for _, s := range sessions {
		for _, p := range s.Panes {
			a, err := client.Activity(ctx, s.Name, p.ID, 256)
			if err != nil {
				if vanished(err) {
					continue
				}
				return err
			}
			for _, e := range a.Entries {
				if h, ok := activity(boot, s.Name, p.ID, e); ok {
					entries = append(entries, h)
				}
			}
			cmds, err := client.CommandHistory(ctx, s.Name, p.ID, 200)
			if err != nil {
				if vanished(err) {
					continue
				}
				return err
			}
			for _, c := range cmds {
				entries = append(entries, command(boot, c)...)
			}
		}
	}
	return st.AddHistory(entries...)
}

func activity(boot, session, window string, e daemon.ActivityEntry) (store.HistoryEntry, bool) {
	kind, label := "", ""
	switch e.Kind {
	case "prompt":
		kind, label = "prompt-submitted", "Prompt submitted"
	case "turn_end":
		kind, label = "response-completed", "Agent response completed"
	case "state":
		kind, label = "agent-state", "Agent state"
	case "session_start", "subagent_start", "subagent_stop":
		kind, label = e.Kind, e.Kind
	default:
		return store.HistoryEntry{}, false
	}
	sourceID := fmt.Sprintf("%s/%s/%d/%d", boot, window, e.Seq, e.At)
	detail := e.Text
	if e.Kind == "turn_end" {
		detail = "Turn-end hook report (may be a summary, not the full response):\n\n" + detail
		if e.Text == "" {
			detail += "The harness reported completion without response text."
		}
	}
	return store.HistoryEntry{ID: "activity/" + sourceID, Source: "agent-activity", SourceID: sourceID,
		Kind: kind, At: e.At / int64(time.Millisecond), Session: session, Window: window,
		Summary: label + ": " + panes.OneLine(e.Text), Detail: detail}, true
}

func command(boot string, c daemon.HistoryEntry) []store.HistoryEntry {
	sourceID := fmt.Sprintf("%s/%s/%d/%d", boot, c.Window, c.Seq, c.At)
	exit := "not reported"
	if c.Exit != nil {
		exit = fmt.Sprint(*c.Exit)
	}
	detail := fmt.Sprintf("%s\n\nWorking directory: %s\nExit: %s\nDuration: %d ms\nCommand sequence: %d", c.Cmdline, c.Cwd, exit, c.DurationMS, c.Seq)
	return []store.HistoryEntry{
		{ID: "command-start/" + sourceID, Source: "command-history", SourceID: sourceID,
			Kind: "command-submitted", At: c.At, Session: c.Session, Window: c.Window, Summary: "Command submitted (retained): " + c.Cmdline, Detail: c.Cmdline + "\n\nStart time retained by shell integration."},
		{ID: "command-end/" + sourceID, Source: "command-history", SourceID: sourceID,
			Kind: "command-completed", At: c.At + c.DurationMS, Session: c.Session, Window: c.Window, Summary: "Command completed (exit " + exit + "): " + c.Cmdline, Detail: detail},
	}
}

func observe(ctx context.Context, client *daemon.Client, st *store.Store, ev daemon.Event) error {
	var raw struct {
		Time       int64                `json:"time"`
		Entry      daemon.ActivityEntry `json:"entry"`
		Cmdline    string               `json:"cmdline"`
		CommandSeq int64                `json:"command_seq"`
	}
	if err := json.Unmarshal(ev.Raw, &raw); err != nil {
		return err
	}
	if ev.Type == "agent-activity" {
		if h, ok := activity(ev.BootID, ev.Session, ev.Window, raw.Entry); ok {
			return st.AddHistory(h)
		}
		return nil
	}
	if ev.Type == "command-finished" {
		cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		cmds, err := client.CommandHistory(cctx, ev.Session, ev.Window, 200)
		if err != nil && !vanished(err) {
			return err
		}
		var entries []store.HistoryEntry
		for _, c := range cmds {
			entries = append(entries, command(ev.BootID, c)...)
		}
		// A pane may close before its command ring can be read. Preserve the event
		// when the matching command has already left the ring instead of inventing it.
		for _, c := range cmds {
			if c.Seq == raw.CommandSeq {
				return st.AddHistory(entries...)
			}
		}
		if err := st.AddHistory(entries...); err != nil {
			return err
		}
	}
	sourceID := fmt.Sprintf("%s/%d", ev.BootID, ev.Seq)
	h := store.HistoryEntry{ID: "event/" + sourceID, Source: "daemon", SourceID: sourceID,
		Kind: ev.Type, At: raw.Time / int64(time.Millisecond), Session: ev.Session, Window: ev.Window,
		Summary: strings.ReplaceAll(ev.Type, "-", " "), Detail: string(ev.Raw)}
	if h.At == 0 {
		h.At = time.Now().UnixMilli()
		h.Detail = "Source has no timestamp; time is when observed.\n" + h.Detail
	}
	if ev.Type == "command-started" {
		h.Summary = "Command started (observed): " + raw.Cmdline
	}
	if ev.Type == "command-finished" {
		h.Kind = "command-completed"
	}
	if ev.Type == "gap" {
		h.Summary = "History gap: " + ev.Reason
	}
	if err := st.AddHistory(h); err != nil {
		return err
	}
	if ev.Type == "gap" {
		return recoverSources(ctx, client, st, ev.BootID)
	}
	return nil
}

func vanished(err error) bool {
	var e *daemon.Error
	return errors.As(err, &e) && (e.Code == "window_not_found" || e.Code == "session_not_found")
}
